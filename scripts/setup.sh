#!/usr/bin/env bash
# First-time bare-metal setup: checks prerequisites, creates the data directory
# and a system service user, writes the env file (auto-generating the admin and
# setup tokens), builds and installs the binary, creates the first application
# admin login (auto-generated password) if none exists via the one-time
# setup-token bootstrap, and installs the systemd unit. Safe to re-run --
# existing env files, users, databases, and admin logins are left alone rather
# than recreated. Generated passwords/tokens are printed once at the end;
# nothing is saved anywhere else, so capture them when they're shown.
#
# Database backend: defaults to embedded SQLite. To use an external PostgreSQL
# database instead, either:
#   * set DATABASE_URL to an existing database
#       (postgres://user:pass@host:5432/db?sslmode=require), or
#   * set PG_ADMIN_URL to a superuser/admin connection and this script will
#     CREATE ROLE/DATABASE for you (using PG_DB/PG_USER/PG_PASSWORD/PG_HOST/
#     PG_PORT/PG_SSLMODE, auto-generating PG_PASSWORD if unset) and assemble
#     DATABASE_URL from them. The application schema itself is created
#     automatically on first boot, so no manual migration step is needed.
set -euo pipefail

APP_NAME="grc"
DATA_DIR="${DATA_DIR:-/var/lib/${APP_NAME}}"
SQLITE_PATH="${SQLITE_PATH:-${DATA_DIR}/users.db}"
ENV_FILE="${GRC_ENV_FILE:-/etc/${APP_NAME}/${APP_NAME}.env}"
BIN_PATH="${GRC_BIN_PATH:-/usr/local/bin/${APP_NAME}}"
SERVICE_USER="${GRC_SERVICE_USER:-${APP_NAME}}"
LISTEN_ADDR="${LISTEN_ADDR:-0.0.0.0:80}"
ALLOW_JSON_SAVE="${ALLOW_JSON_SAVE:-false}"
ADMIN_USER="${ADMIN_USER:-admin}"

# Optional external tooling. Both are opt-in because installing software on a
# server should be a deliberate choice, not a side effect of running setup.
#
#   INSTALL_TYPST=1   fetch the Typst binary so templates/build.sh can render
#                     policy and report PDFs on this host (see
#                     DOCUMENT_TEMPLATES.md). Single static binary, no deps.
#   INSTALL_CHROME=1  install chromium for the /reports HTML-to-PDF module via
#                     the system package manager.
INSTALL_TYPST="${INSTALL_TYPST:-0}"
INSTALL_CHROME="${INSTALL_CHROME:-0}"
TYPST_VERSION="${TYPST_VERSION:-latest}"

# PostgreSQL backend (optional). USE_POSTGRES is derived: Postgres is selected
# when either an explicit DATABASE_URL or a PG_ADMIN_URL (provisioning) is given.
DATABASE_URL="${DATABASE_URL:-}"
PG_ADMIN_URL="${PG_ADMIN_URL:-}"
PG_HOST="${PG_HOST:-localhost}"
PG_PORT="${PG_PORT:-5432}"
PG_DB="${PG_DB:-${APP_NAME}}"
PG_USER="${PG_USER:-${APP_NAME}}"
PG_PASSWORD="${PG_PASSWORD:-}"
PG_SSLMODE="${PG_SSLMODE:-disable}"

USE_POSTGRES=0
if [ -n "$DATABASE_URL" ] || [ -n "$PG_ADMIN_URL" ]; then
  USE_POSTGRES=1
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

echo "==> Checking prerequisites"
for tool in go curl openssl; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "error: $tool is not installed; install it and re-run." >&2
    exit 1
  fi
done
if [ -n "$PG_ADMIN_URL" ] && ! command -v psql >/dev/null 2>&1; then
  echo "error: PG_ADMIN_URL is set but psql is not installed; install" >&2
  echo "       postgresql-client and re-run." >&2
  exit 1
fi
echo "    $(go version)"
if [ "$USE_POSTGRES" = 1 ]; then
  echo "    database backend: PostgreSQL"
else
  echo "    database backend: SQLite ($SQLITE_PATH)"
fi

echo "==> Building ${APP_NAME}"
# Build as the invoking user (so the build uses their Go module/cache and
# credentials), then install into BIN_PATH with sudo -- BIN_PATH is typically
# under /usr/local/bin, which the normal user can't write to.
BUILD_TMP="$(mktemp)"
trap 'rm -f "$BUILD_TMP"' EXIT
# Stamp the source revision into the binary so scripts/verify-install.sh can
# tell a current deploy from a stale one. Nothing else can: pageAccessMiddleware
# guards whole path prefixes and aborts before routing, so probing for a new
# route returns the same 401 whether or not that route exists.
BUILD_REV="$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo dev)"
# CGO_ENABLED=0 because nothing here needs a C compiler any more: the SQLite
# driver is pure Go, which also means the server this runs on does not need a
# toolchain installed. -tags fts5 is gone with the C driver — that driver left
# full-text search out unless asked for it, and this one always compiles it in.
(cd "$REPO_ROOT" && CGO_ENABLED=0 go build \
  -ldflags "-X grc/internal/app.BuildVersion=${BUILD_REV}" \
  -o "$BUILD_TMP" ./cmd/api)
sudo install -m 0755 "$BUILD_TMP" "$BIN_PATH"
echo "    installed binary at $BIN_PATH (revision ${BUILD_REV})"

# The /changelog and /knowledge/* pages read the repository markdown files from
# disk, and the service runs with "/" as its working directory. Without a copy
# next to the binary the change log comes up empty on every deployed server.
# This path is one of the directories internal/app/docs.go searches, so no
# configuration is needed; DOCS_DIR overrides it.
DOC_DIR="${GRC_DOC_DIR:-$(dirname "$(dirname "$BIN_PATH")")/share/${APP_NAME}}"
echo "==> Installing documentation to $DOC_DIR"
sudo install -d -m 0755 "$DOC_DIR"
sudo install -m 0644 "$REPO_ROOT"/*.md "$DOC_DIR/"
echo "    installed $(find "$REPO_ROOT" -maxdepth 1 -name '*.md' | wc -l) markdown files"

echo "==> Ensuring system user '$SERVICE_USER' exists"
if ! id "$SERVICE_USER" >/dev/null 2>&1; then
  sudo useradd --system --no-create-home --shell /usr/sbin/nologin "$SERVICE_USER"
  echo "    created system user '$SERVICE_USER'"
else
  echo "    system user '$SERVICE_USER' already exists"
fi

echo "==> Ensuring data directory at $DATA_DIR"
sudo mkdir -p "$DATA_DIR"
sudo chown "$SERVICE_USER":"$SERVICE_USER" "$DATA_DIR"
sudo chmod 0700 "$DATA_DIR"

if [ "$USE_POSTGRES" = 1 ] && [ -n "$PG_ADMIN_URL" ]; then
  echo "==> Provisioning PostgreSQL role and database via PG_ADMIN_URL"
  if [ -z "$PG_PASSWORD" ]; then
    PG_PASSWORD="$(openssl rand -hex 16)"
    PG_PASSWORD_GENERATED=1
  fi
  # Role: create if missing, and (re)assert the login password either way so the
  # assembled DATABASE_URL is guaranteed to authenticate. The password is hex
  # (generated) or operator-supplied; keep it free of single quotes.
  if psql "$PG_ADMIN_URL" -tAc "SELECT 1 FROM pg_roles WHERE rolname = '${PG_USER}'" | grep -q 1; then
    echo "    role '$PG_USER' already exists"
  else
    psql "$PG_ADMIN_URL" -v ON_ERROR_STOP=1 -q \
      -c "CREATE ROLE \"${PG_USER}\" LOGIN PASSWORD '${PG_PASSWORD}'"
    echo "    created role '$PG_USER'"
  fi
  if [ -n "${PG_PASSWORD_GENERATED:-}" ]; then
    psql "$PG_ADMIN_URL" -v ON_ERROR_STOP=1 -q \
      -c "ALTER ROLE \"${PG_USER}\" LOGIN PASSWORD '${PG_PASSWORD}'"
  fi
  # Database: CREATE DATABASE cannot run inside a transaction, so gate it with a
  # separate existence check rather than a DO block.
  if psql "$PG_ADMIN_URL" -tAc "SELECT 1 FROM pg_database WHERE datname = '${PG_DB}'" | grep -q 1; then
    echo "    database '$PG_DB' already exists"
  else
    psql "$PG_ADMIN_URL" -v ON_ERROR_STOP=1 -q \
      -c "CREATE DATABASE \"${PG_DB}\" OWNER \"${PG_USER}\""
    echo "    created database '$PG_DB' owned by '$PG_USER'"
  fi
fi

if [ "$USE_POSTGRES" = 1 ] && [ -z "$DATABASE_URL" ]; then
  DATABASE_URL="postgres://${PG_USER}:${PG_PASSWORD}@${PG_HOST}:${PG_PORT}/${PG_DB}?sslmode=${PG_SSLMODE}"
fi

if [ "$USE_POSTGRES" = 1 ]; then
  DB_CONFIG_LINE="DATABASE_URL=$DATABASE_URL"
else
  DB_CONFIG_LINE="SQLITE_PATH=$SQLITE_PATH"
fi

echo "==> Writing environment file at $ENV_FILE"
sudo mkdir -p "$(dirname "$ENV_FILE")"
if [ ! -f "$ENV_FILE" ]; then
  ADMIN_TOKEN="$(openssl rand -hex 32)"
  SETUP_TOKEN="$(openssl rand -hex 32)"
  TOKENS_GENERATED=1
  sudo tee "$ENV_FILE" >/dev/null <<EOF_ENV
$DB_CONFIG_LINE
LISTEN_ADDR=$LISTEN_ADDR
ALLOW_JSON_SAVE=$ALLOW_JSON_SAVE
ADMIN_TOKEN=$ADMIN_TOKEN
SETUP_TOKEN=$SETUP_TOKEN
WINTERMUTE_URL=
WINTERMUTE_TOKEN=
WINTERMUTE_BACKEND=
WINTERMUTE_MODEL=
EOF_ENV
  echo "    wrote $ENV_FILE"
else
  echo "    $ENV_FILE already exists, leaving it alone"
  # Pull the existing setup token and database config so the admin bootstrap
  # below talks to the same backend the service will use.
  SETUP_TOKEN="$(sudo sh -c "set -a; . '$ENV_FILE'; printf '%s' \"\${SETUP_TOKEN:-}\"")"
  EXISTING_DATABASE_URL="$(sudo sh -c "set -a; . '$ENV_FILE'; printf '%s' \"\${DATABASE_URL:-}\"")"
  if [ -n "$EXISTING_DATABASE_URL" ]; then
    DATABASE_URL="$EXISTING_DATABASE_URL"
    USE_POSTGRES=1
  fi
fi
sudo chown "$SERVICE_USER":"$SERVICE_USER" "$ENV_FILE"
sudo chmod 0600 "$ENV_FILE"

echo "==> Ensuring an application admin login exists"
# grc has no create-user CLI; the first admin is provisioned
# over HTTP via POST /auth/bootstrap-admin guarded by SETUP_TOKEN. Briefly run
# the installed binary as the service user against the real database on a
# loopback port, post the bootstrap request (201 = created, 409 = an admin
# already exists), then stop it. The schema (SQLite or Postgres) is created on
# this startup, so no separate migration step is needed.
if [ -z "${SETUP_TOKEN:-}" ]; then
  echo "    no SETUP_TOKEN in $ENV_FILE; skipping admin bootstrap"
  echo "    (set SETUP_TOKEN there and re-run, or use /admin/user-management)"
else
  BOOT_ADDR="127.0.0.1:8099"
  BOOT_LOG="$(mktemp)"
  BOOT_BODY="$(mktemp)"
  ADMIN_PASS="$(openssl rand -hex 12)"
  if [ "$USE_POSTGRES" = 1 ]; then
    DB_BOOT_ENV=(DATABASE_URL="$DATABASE_URL")
  else
    DB_BOOT_ENV=(SQLITE_PATH="$SQLITE_PATH")
  fi
  sudo -u "$SERVICE_USER" env \
    "${DB_BOOT_ENV[@]}" LISTEN_ADDR="$BOOT_ADDR" SETUP_TOKEN="$SETUP_TOKEN" \
    "$BIN_PATH" >"$BOOT_LOG" 2>&1 &
  BOOT_PID=$!

  ready=0
  for _ in $(seq 1 30); do
    if curl -sf -o /dev/null "http://${BOOT_ADDR}/health" 2>/dev/null; then
      ready=1
      break
    fi
    if ! kill -0 "$BOOT_PID" 2>/dev/null; then
      break
    fi
    sleep 0.5
  done

  if [ "$ready" != "1" ]; then
    echo "    bootstrap server did not become ready; see below" >&2
    cat "$BOOT_LOG" >&2 || true
    kill "$BOOT_PID" 2>/dev/null || true
    wait "$BOOT_PID" 2>/dev/null || true
    rm -f "$BOOT_LOG" "$BOOT_BODY"
    exit 1
  fi

  BOOT_STATUS="$(curl -sS -o "$BOOT_BODY" -w "%{http_code}" \
    -X POST "http://${BOOT_ADDR}/auth/bootstrap-admin" \
    -H 'Content-Type: application/json' \
    -H "X-Setup-Token: ${SETUP_TOKEN}" \
    -d "{\"username\":\"${ADMIN_USER}\",\"password\":\"${ADMIN_PASS}\"}")"

  kill "$BOOT_PID" 2>/dev/null || true
  wait "$BOOT_PID" 2>/dev/null || true

  case "$BOOT_STATUS" in
    201)
      ADMIN_CREATED=1
      echo "    created application admin login '$ADMIN_USER'"
      ;;
    409)
      echo "    an admin already exists, skipping admin creation"
      ;;
    *)
      echo "    bootstrap failed (HTTP $BOOT_STATUS): $(cat "$BOOT_BODY")" >&2
      rm -f "$BOOT_LOG" "$BOOT_BODY"
      exit 1
      ;;
  esac
  rm -f "$BOOT_LOG" "$BOOT_BODY"
fi

if [ "$INSTALL_TYPST" = 1 ]; then
  echo "==> Installing Typst (document template rendering)"
  if command -v typst >/dev/null 2>&1; then
    echo "    typst already installed ($(typst --version 2>/dev/null))"
  else
    case "$(uname -m)" in
      x86_64)          TYPST_ARCH="x86_64" ;;
      aarch64|arm64)   TYPST_ARCH="aarch64" ;;
      *)               echo "    unsupported architecture $(uname -m); skipping" ; TYPST_ARCH="" ;;
    esac
    if [ -n "$TYPST_ARCH" ]; then
      # musl build: statically linked, so it does not care which glibc the
      # distribution ships.
      TYPST_TARBALL="typst-${TYPST_ARCH}-unknown-linux-musl"
      if [ "$TYPST_VERSION" = "latest" ]; then
        TYPST_URL="https://github.com/typst/typst/releases/latest/download/${TYPST_TARBALL}.tar.xz"
      else
        TYPST_URL="https://github.com/typst/typst/releases/download/${TYPST_VERSION}/${TYPST_TARBALL}.tar.xz"
      fi
      TYPST_TMP="$(mktemp -d)"
      if curl -sSL --max-time 180 -o "$TYPST_TMP/typst.tar.xz" "$TYPST_URL" \
         && tar -xf "$TYPST_TMP/typst.tar.xz" -C "$TYPST_TMP"; then
        sudo install -m 0755 "$TYPST_TMP/$TYPST_TARBALL/typst" /usr/local/bin/typst
        echo "    installed $(typst --version 2>/dev/null) at /usr/local/bin/typst"
      else
        echo "    warning: could not download Typst from $TYPST_URL" >&2
        echo "    (templates/build.sh will not render PDFs on this host)" >&2
      fi
      rm -rf "$TYPST_TMP"
    fi
  fi
fi

if [ "$INSTALL_CHROME" = 1 ]; then
  echo "==> Installing Chromium (reporting module HTML-to-PDF)"
  if command -v google-chrome >/dev/null 2>&1 || command -v chromium >/dev/null 2>&1 \
     || command -v chromium-browser >/dev/null 2>&1; then
    echo "    a Chrome/Chromium binary is already present"
  elif command -v apt-get >/dev/null 2>&1; then
    sudo apt-get update -qq && sudo apt-get install -y -qq chromium || \
      sudo apt-get install -y -qq chromium-browser || \
      echo "    warning: chromium install failed; set REPORTING_CHROME_PATH manually" >&2
  elif command -v dnf >/dev/null 2>&1; then
    sudo dnf install -y -q chromium || \
      echo "    warning: chromium install failed; set REPORTING_CHROME_PATH manually" >&2
  else
    echo "    no supported package manager found; install chromium manually" >&2
  fi
fi

echo "==> Installing systemd unit"
sudo cp "$REPO_ROOT/deploy/${APP_NAME}.service" "/etc/systemd/system/${APP_NAME}.service"
sudo sed -i \
  -e "s|^User=.*|User=$SERVICE_USER|" \
  -e "s|^Group=.*|Group=$SERVICE_USER|" \
  -e "s|^EnvironmentFile=.*|EnvironmentFile=$ENV_FILE|" \
  -e "s|^ExecStart=.*|ExecStart=$BIN_PATH|" \
  "/etc/systemd/system/${APP_NAME}.service"
sudo systemctl daemon-reload
sudo systemctl enable "$APP_NAME"

if [ -n "${TOKENS_GENERATED:-}" ] || [ -n "${ADMIN_CREATED:-}" ] || [ -n "${PG_PASSWORD_GENERATED:-}" ]; then
  echo
  echo "==> Generated credentials (shown once here only -- save them now)"
  if [ -n "${TOKENS_GENERATED:-}" ]; then
    echo "    Admin bearer token (ADMIN_TOKEN): $ADMIN_TOKEN"
    echo "    First-boot setup token (SETUP_TOKEN): $SETUP_TOKEN"
  fi
  if [ -n "${PG_PASSWORD_GENERATED:-}" ]; then
    echo "    PostgreSQL role '$PG_USER' password: $PG_PASSWORD"
    echo "    (already baked into DATABASE_URL in $ENV_FILE)"
  fi
  if [ -n "${ADMIN_CREATED:-}" ]; then
    echo "    App admin login: $ADMIN_USER / $ADMIN_PASS"
  fi
fi

echo
echo "==> Starting $APP_NAME"
sudo systemctl restart "$APP_NAME"
# Give the unit a moment to bind before verifying, so the check reports the real
# state rather than a race.
for _ in $(seq 1 20); do
  systemctl is-active --quiet "$APP_NAME" && break
  sleep 0.5
done

echo
echo "==> Verifying the installation"
# Proves the deployed binary actually serves the routes the current source
# registers and that the schema carries the current tables. A stale binary
# starts cleanly and only 404s when somebody clicks a new page, so this turns
# that into a setup-time failure.
if ! "$REPO_ROOT/scripts/verify-install.sh"; then
  echo
  echo "Setup finished but verification failed — see above." >&2
  exit 1
fi

echo
echo "Setup complete. Next steps:"
echo "  1. (Optional) Edit $ENV_FILE to set the WINTERMUTE_* vars for the AI chat assistant."
echo "  2. Check status:  sudo systemctl status $APP_NAME"
echo "  3. Tail logs:     sudo journalctl -u $APP_NAME -f"
echo "  4. Once an admin exists you may blank SETUP_TOKEN in $ENV_FILE and restart."
echo "  5. Non-admin users need the new pages granted explicitly — admins bypass"
echo "     page access, everyone else does not:"
echo "       scripts/manage-user.sh update -u USER --pages '/controls,/policies,/policies/coverage'"
echo "     Granting a page also grants its supporting /data endpoints; use"
echo "     '/policies/*' to include the editor as well."
echo "  6. Render document templates on this host (needs typst):"
echo "       cd templates && ./build.sh"
echo "     See DOCUMENT_TEMPLATES.md. Re-run setup with INSTALL_TYPST=1 to add it."
echo
