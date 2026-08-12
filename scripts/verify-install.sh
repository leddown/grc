#!/usr/bin/env bash
# Post-install verification: proves that a deployed grc actually
# serves the routes the current source registers, that the schema carries the
# current tables, and that the optional external tooling some features need is
# present.
#
# Run automatically at the end of scripts/setup.sh and update.sh, and safe to
# run on its own at any time:
#
#   ./scripts/verify-install.sh
#
# It is read-only against the application: every request is a GET and no row is
# written. The one exception is schema creation, which happens on startup and is
# idempotent (CREATE TABLE IF NOT EXISTS) — that is exactly what needs verifying
# after an upgrade that added tables.
#
# Why this exists: schema changes and new routes are applied automatically on
# startup, so a stale binary or a half-finished deploy fails silently — the
# service starts, /health returns 200, and the new pages 404 only when somebody
# clicks them. This turns that into a deploy-time failure.
#
# Exit status: 0 if every required check passed, 1 otherwise. Optional tooling
# produces warnings and never fails the run.
set -uo pipefail

APP_NAME="grc"
ENV_FILE="${GRC_ENV_FILE:-/etc/${APP_NAME}/${APP_NAME}.env}"
BIN_PATH="${GRC_BIN_PATH:-/usr/local/bin/${APP_NAME}}"
SERVICE_NAME="${GRC_SERVICE_NAME:-${APP_NAME}}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

PASS=0
FAIL=0
WARN=0
TEMP_PID=""
TEMP_LOG=""

ok()   { printf '    \033[32m✓\033[0m %s\n' "$1"; PASS=$((PASS + 1)); }
bad()  { printf '    \033[31m✗\033[0m %s\n' "$1"; FAIL=$((FAIL + 1)); }
warn() { printf '    \033[33m!\033[0m %s\n' "$1"; WARN=$((WARN + 1)); }

cleanup() {
  if [ -n "$TEMP_PID" ]; then
    kill "$TEMP_PID" 2>/dev/null || true
    wait "$TEMP_PID" 2>/dev/null || true
  fi
  [ -n "$TEMP_LOG" ] && rm -f "$TEMP_LOG"
}
trap cleanup EXIT

# ---------------------------------------------------------------------------
# Locate a running instance, or start a temporary one
# ---------------------------------------------------------------------------

read_env() {
  # Values live in a root-owned 0600 file, so read them through sudo. -n means
  # "never prompt": this script runs from deploy hooks and non-interactive
  # shells where a password prompt would hang the deploy rather than fail it.
  # Without cached credentials the reads simply come back empty and the checks
  # that need them degrade to warnings.
  sudo -n sh -c "set -a; . '$ENV_FILE' 2>/dev/null; printf '%s' \"\${$1:-}\"" 2>/dev/null
}

# Where the markdown files /changelog and /knowledge/* read should be installed.
# Computed here rather than at the documentation check below so a temporary
# instance can be started with the same DOCS_DIR the service uses: without it
# the temporary process inherits this script's working directory, finds the
# files in the checkout, and reports a healthy change log on a host where the
# real service cannot see them.
DOCS_DIR_ENV="$(read_env DOCS_DIR)"
if [ -n "$DOCS_DIR_ENV" ]; then
  DOC_DIR="$DOCS_DIR_ENV"
  DOC_DIR_SOURCE="DOCS_DIR in $ENV_FILE"
else
  DOC_DIR="${GRC_DOC_DIR:-$(dirname "$(dirname "$BIN_PATH")")/share/${APP_NAME}}"
  DOC_DIR_SOURCE="installed alongside $BIN_PATH"
fi

echo "==> Locating the application"

BASE_URL=""
if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
  LISTEN_ADDR="$(read_env LISTEN_ADDR)"
  [ -z "$LISTEN_ADDR" ] && LISTEN_ADDR="127.0.0.1:8081"
  # 0.0.0.0 / :: are bind addresses, not dial addresses.
  PROBE_ADDR="${LISTEN_ADDR/#0.0.0.0:/127.0.0.1:}"
  PROBE_ADDR="${PROBE_ADDR/#:::/127.0.0.1:}"
  case "$PROBE_ADDR" in
    :*) PROBE_ADDR="127.0.0.1${PROBE_ADDR}" ;;
  esac
  if curl -sf -o /dev/null --max-time 5 "http://${PROBE_ADDR}/health" 2>/dev/null; then
    BASE_URL="http://${PROBE_ADDR}"
    echo "    verifying the running $SERVICE_NAME service at $BASE_URL"
  else
    echo "    $SERVICE_NAME is active but did not answer on $PROBE_ADDR"
  fi
fi

if [ -z "$BASE_URL" ]; then
  if [ ! -x "$BIN_PATH" ]; then
    echo "error: no running service and no binary at $BIN_PATH" >&2
    exit 1
  fi
  echo "    no reachable service; starting a temporary instance"
  # An explicitly exported DATABASE_URL/SQLITE_PATH wins over the env file, so
  # this can be pointed at a scratch database for a dry run.
  DATABASE_URL="${DATABASE_URL:-$(read_env DATABASE_URL)}"
  SQLITE_PATH="${SQLITE_PATH:-$(read_env SQLITE_PATH)}"
  [ -z "$SQLITE_PATH" ] && SQLITE_PATH="/var/lib/${APP_NAME}/users.db"

  if [ -n "$DATABASE_URL" ]; then
    DB_ENV=(DATABASE_URL="$DATABASE_URL")
  else
    DB_ENV=(SQLITE_PATH="$SQLITE_PATH")
  fi

  TEMP_ADDR="127.0.0.1:8098"
  TEMP_LOG="$(mktemp)"
  SERVICE_USER="${GRC_SERVICE_USER:-$APP_NAME}"
  if id "$SERVICE_USER" >/dev/null 2>&1; then
    sudo -u "$SERVICE_USER" env "${DB_ENV[@]}" LISTEN_ADDR="$TEMP_ADDR" DOCS_DIR="$DOC_DIR" \
      "$BIN_PATH" >"$TEMP_LOG" 2>&1 &
  else
    env "${DB_ENV[@]}" LISTEN_ADDR="$TEMP_ADDR" DOCS_DIR="$DOC_DIR" "$BIN_PATH" >"$TEMP_LOG" 2>&1 &
  fi
  TEMP_PID=$!

  for _ in $(seq 1 40); do
    curl -sf -o /dev/null "http://${TEMP_ADDR}/health" 2>/dev/null && break
    kill -0 "$TEMP_PID" 2>/dev/null || break
    sleep 0.5
  done
  if ! curl -sf -o /dev/null "http://${TEMP_ADDR}/health" 2>/dev/null; then
    echo "error: temporary instance did not become ready:" >&2
    cat "$TEMP_LOG" >&2
    exit 1
  fi
  BASE_URL="http://${TEMP_ADDR}"
  echo "    temporary instance ready at $BASE_URL"
fi

status_of() { curl -s -o /dev/null -w '%{http_code}' --max-time 10 "$1" 2>/dev/null; }

# route_reachable checks that a path answers and does not fault.
#
# It deliberately does NOT claim to prove a route is registered. pageAccessMiddleware
# guards whole prefixes (/controls, /policies, ...) and aborts before gin routes
# the request, so an unregistered path under a guarded prefix returns exactly the
# same 401 as a registered one. Detecting a stale binary is the version check's
# job, not this one's; this catches 5xx and unreachability.
route_reachable() {
  local path="$1" label="$2" code
  code="$(status_of "${BASE_URL}${path}")"
  case "$code" in
    200|201|204|301|302) ok "$label ($path → $code)" ;;
    401|403)             ok "$label ($path → $code, auth-gated)" ;;
    404)                 bad "$label MISSING ($path → 404)" ;;
    000)                 bad "$label unreachable ($path)" ;;
    5*)                  bad "$label faulted ($path → $code)" ;;
    *)                   warn "$label unexpected status ($path → $code)" ;;
  esac
}

# ---------------------------------------------------------------------------
# Core surface
# ---------------------------------------------------------------------------

# ---------------------------------------------------------------------------
# Build revision — the check that actually detects a stale deploy
# ---------------------------------------------------------------------------

echo
echo "==> Build revision"

DEPLOYED_VERSION="$(curl -s --max-time 10 "${BASE_URL}/version" 2>/dev/null \
  | sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"

if [ -z "$DEPLOYED_VERSION" ]; then
  bad "the running binary has no /version endpoint — it predates build stamping"
  bad "  rebuild and restart:  ./update.sh"
elif [ "$DEPLOYED_VERSION" = "dev" ]; then
  warn "running an unstamped build (version=dev)"
  warn "  built with a plain 'go build'; setup.sh and update.sh stamp the commit"
elif git -C "$REPO_ROOT" rev-parse --git-dir >/dev/null 2>&1; then
  SOURCE_VERSION="$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null)"
  if [ "$DEPLOYED_VERSION" = "$SOURCE_VERSION" ]; then
    ok "deployed build matches the checkout ($DEPLOYED_VERSION)"
  else
    bad "STALE BINARY: serving $DEPLOYED_VERSION, checkout is at $SOURCE_VERSION"
    bad "  the service is running older code than this repository"
    bad "  rebuild and restart:  ./update.sh"
  fi
else
  warn "deployed build is $DEPLOYED_VERSION (no git checkout here to compare against)"
fi

echo
echo "==> Core routes"
route_reachable /health           "health endpoint"
route_reachable /                 "home page"
route_reachable /controls         "control catalog"
route_reachable /security-nfrs    "security NFR catalog"
route_reachable /reports          "reports"

echo
echo "==> Policy module routes"
route_reachable /policies                "policy library"
route_reachable /policies/manage         "policy editor"
route_reachable /policies/coverage       "policy coverage matrix"
route_reachable /policies/meta           "policy editor vocabularies"
route_reachable /policies/data           "policy list data"
route_reachable /policies/coverage/data  "coverage report data"
route_reachable /policies/control-search "control picker search"

echo
echo "==> Control catalog detail endpoint"
route_reachable /controls/data/AC-1 "per-control record"

# ---------------------------------------------------------------------------
# Behavioural checks — things a route-existence probe would not catch
# ---------------------------------------------------------------------------

echo
echo "==> Behaviour"

# A page number large enough to overflow the pagination offset used to panic the
# handler and return 500.
#
# On an authenticated deployment this probe is turned away by the auth
# middleware before it reaches the handler, so a 401 proves nothing about the
# fix and is reported as such rather than as a pass — a check that always
# succeeds is worse than no check. A 5xx is still a real failure either way,
# which is the signal worth having here. The fix itself is covered properly by
# internal/apiutil/pagination_test.go.
OVERFLOW_CODE="$(status_of "${BASE_URL}/controls/data?page=9223372036854775807&per_page=500")"
case "$OVERFLOW_CODE" in
  5*)          bad  "pagination overflow faults (page=MaxInt64 → $OVERFLOW_CODE)" ;;
  000)         bad  "pagination overflow probe unreachable" ;;
  401|403|302) warn "pagination overflow not exercised (auth returned $OVERFLOW_CODE before the handler)" ;;
  *)           ok   "pagination overflow handled (page=MaxInt64 → $OVERFLOW_CODE)" ;;
esac

# The theme middleware rewrites every HTML response. If the injection broke, the
# page still renders but loses its palette and the responsive layer.
HOME_HTML="$(curl -s --max-time 10 "${BASE_URL}/" 2>/dev/null)"
if printf '%s' "$HOME_HTML" | grep -q 'id="global-theme-style"'; then
  ok "theme palette injected"
else
  bad "theme palette missing from the home page"
fi
if printf '%s' "$HOME_HTML" | grep -q 'id="global-mobile-style"'; then
  ok "responsive/iOS layer injected"
else
  bad "responsive layer missing from the home page"
fi

# ---------------------------------------------------------------------------
# Documentation files
# ---------------------------------------------------------------------------

echo
echo "==> Documentation"

# /changelog and /knowledge/* read markdown files from disk. The service runs
# with "/" as its working directory, so unless the files are installed where
# internal/app/docs.go looks for them the change log renders empty and every
# knowledge page 404s — with the service otherwise perfectly healthy, which is
# why this needs checking at deploy time rather than when somebody clicks it.
if [ -f "$DOC_DIR/CHANGELOG.md" ]; then
  ok "CHANGELOG.md present in $DOC_DIR ($DOC_DIR_SOURCE)"
  # An out-of-date copy is the documentation equivalent of a stale binary: the
  # page renders, and shows the wrong history.
  if [ -f "$REPO_ROOT/CHANGELOG.md" ] && ! cmp -s "$REPO_ROOT/CHANGELOG.md" "$DOC_DIR/CHANGELOG.md"; then
    bad "installed CHANGELOG.md differs from the checkout — the served change log is stale"
    bad "  reinstall the docs:  ./update.sh"
  fi
  MISSING_DOCS=""
  for doc in Agents.md RUNTIME_ARGS.md FAQ.md JIRA_CONNECTOR.md RISK_REGISTER_FRAMEWORK.md \
             POLICY_MODULE_FRAMEWORK.md DOCUMENT_TEMPLATES.md; do
    [ -f "$DOC_DIR/$doc" ] || MISSING_DOCS="$MISSING_DOCS $doc"
  done
  if [ -n "$MISSING_DOCS" ]; then
    warn "knowledge pages missing their source files:$MISSING_DOCS"
    warn "  those /knowledge/* pages will 404; reinstall with ./update.sh"
  else
    ok "all /knowledge/* source files present"
  fi
else
  bad "no CHANGELOG.md in $DOC_DIR — /changelog will render empty"
  bad "  install the docs:  ./update.sh   (or set DOCS_DIR in $ENV_FILE)"
fi

# The served page is the thing that actually matters, but /changelog sits behind
# pageAccessMiddleware, so on an authenticated deployment this probe is turned
# away before the handler and proves nothing — reported as such rather than as a
# pass, same as the pagination probe above.
CHANGELOG_CODE="$(status_of "${BASE_URL}/changelog")"
case "$CHANGELOG_CODE" in
  200)
    if curl -s --max-time 10 "${BASE_URL}/changelog" 2>/dev/null | grep -q 'No change log entries found.'; then
      bad "/changelog renders the empty state — the app cannot read CHANGELOG.md"
    else
      ok "/changelog serves change log content"
    fi
    ;;
  401|403|302) warn "/changelog content not exercised (auth returned $CHANGELOG_CODE before the handler)" ;;
  000)         bad  "/changelog unreachable" ;;
  5*)          bad  "/changelog faulted ($CHANGELOG_CODE)" ;;
  *)           warn "/changelog unexpected status ($CHANGELOG_CODE)" ;;
esac

# ---------------------------------------------------------------------------
# Schema
# ---------------------------------------------------------------------------

echo
echo "==> Schema"

DATABASE_URL="${DATABASE_URL:-$(read_env DATABASE_URL)}"
SQLITE_PATH="${SQLITE_PATH:-$(read_env SQLITE_PATH)}"
[ -z "$SQLITE_PATH" ] && SQLITE_PATH="/var/lib/${APP_NAME}/users.db"

REQUIRED_TABLES="rcsa_controls security_nfrs auth_users policy_documents policy_sections policy_versions policy_section_controls"

if [ -n "$DATABASE_URL" ]; then
  if command -v psql >/dev/null 2>&1; then
    for table in $REQUIRED_TABLES; do
      if psql "$DATABASE_URL" -tAc "SELECT to_regclass('public.${table}')" 2>/dev/null | grep -q "$table"; then
        ok "table $table"
      else
        bad "table $table missing"
      fi
    done
  else
    warn "psql not installed; cannot verify the PostgreSQL schema directly"
    warn "  (the app creates it on startup, and the route checks above passed)"
  fi
elif command -v sqlite3 >/dev/null 2>&1; then
  if [ -r "$SQLITE_PATH" ] || sudo -n test -r "$SQLITE_PATH" 2>/dev/null; then
    if [ -r "$SQLITE_PATH" ]; then
      EXISTING="$(sqlite3 "$SQLITE_PATH" "SELECT name FROM sqlite_master WHERE type='table'" 2>/dev/null)"
    else
      EXISTING="$(sudo -n sqlite3 "$SQLITE_PATH" "SELECT name FROM sqlite_master WHERE type='table'" 2>/dev/null)"
    fi
    for table in $REQUIRED_TABLES; do
      if printf '%s\n' "$EXISTING" | grep -qx "$table"; then
        ok "table $table"
      else
        bad "table $table missing"
      fi
    done
  else
    warn "cannot read $SQLITE_PATH; skipping direct schema check"
  fi
else
  warn "sqlite3 not installed; cannot verify the SQLite schema directly"
  warn "  (the app creates it on startup, and the route checks above passed)"
fi

# ---------------------------------------------------------------------------
# Optional tooling
# ---------------------------------------------------------------------------

echo
echo "==> Optional tooling"

# Document templates (templates/, DOCUMENT_TEMPLATES.md). Typst is only needed
# to render policy/report PDFs on this host; the application itself does not
# call it yet, so its absence is a warning.
if command -v typst >/dev/null 2>&1; then
  ok "typst $(typst --version 2>/dev/null | awk '{print $2}') — document templates can be rendered here"
else
  warn "typst not installed — templates/build.sh cannot render PDFs on this host"
  warn "  install: scripts/setup.sh INSTALL_TYPST=1, or see DOCUMENT_TEMPLATES.md"
fi

# The reporting module shells out to headless Chrome for HTML-to-PDF.
if command -v google-chrome >/dev/null 2>&1 || command -v chromium >/dev/null 2>&1 \
   || command -v chromium-browser >/dev/null 2>&1 || [ -n "$(read_env REPORTING_CHROME_PATH)" ]; then
  ok "headless Chrome available for the reporting module"
else
  warn "no Chrome/Chromium found — /reports PDF rendering will fail"
  warn "  install chromium, or set REPORTING_CHROME_PATH in $ENV_FILE"
fi

# SQLite full-text search is a build tag, and its absence only shows up at
# runtime when corpus search first runs. /version reports it so a binary built
# without -tags fts5 is caught here instead.
FTS5="$(curl -s --max-time 10 "${BASE_URL}/version" 2>/dev/null \
  | sed -n 's/.*"fts5"[[:space:]]*:[[:space:]]*\([a-z]*\).*/\1/p')"
case "$FTS5" in
  true)  ok "SQLite FTS5 compiled in (corpus search will work)" ;;
  false) warn "binary built without -tags fts5 — SQLite full-text search unavailable"
         warn "  rebuild via setup.sh/update.sh, which pass the tag" ;;
  *)     warn "could not determine FTS5 support from /version" ;;
esac

if [ -d "$REPO_ROOT/templates" ]; then
  ok "document templates present at $REPO_ROOT/templates"
else
  warn "templates/ not found in $REPO_ROOT (expected in a git checkout)"
fi

# ---------------------------------------------------------------------------
echo
if [ "$FAIL" -gt 0 ]; then
  echo "==> FAILED: $PASS passed, $FAIL failed, $WARN warning(s)"
  echo
  echo "A stale build revision, a missing route or a missing table all mean the"
  echo "same thing: the service is not running the current source. Rebuild and"
  echo "restart, then re-check:"
  echo "  ./update.sh"
  echo "  ./scripts/verify-install.sh"
  exit 1
fi
echo "==> OK: $PASS passed, $WARN warning(s)"
