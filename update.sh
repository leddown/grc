#!/usr/bin/env bash
# Redeploys grc after a code change: pulls the latest commit,
# rebuilds the binary, and restarts the systemd service. Run this on the host
# after `git push`-ing changes, or any time the working tree has commits not
# yet reflected in the running service. Assumes scripts/setup.sh has already
# been run once (binary, env file, and systemd unit already exist).
# Schema changes are applied automatically on startup -- no separate migration
# step is needed.
set -euo pipefail

ENV_FILE="${GRC_ENV_FILE:-/etc/grc/grc.env}"
BIN_PATH="${GRC_BIN_PATH:-/usr/local/bin/grc}"
SERVICE_NAME="${GRC_SERVICE_NAME:-grc}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "==> Pulling latest changes"
git -C "$REPO_ROOT" pull --ff-only

echo "==> Building grc"
BUILD_TMP="$(mktemp)"
trap 'rm -f "$BUILD_TMP"' EXIT
# Stamp the revision so verify-install.sh below can prove the running service is
# actually serving this commit rather than the one it started with.
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
DOC_DIR="${GRC_DOC_DIR:-$(dirname "$(dirname "$BIN_PATH")")/share/grc}"
echo "==> Installing documentation to $DOC_DIR"
sudo install -d -m 0755 "$DOC_DIR"
sudo install -m 0644 "$REPO_ROOT"/*.md "$DOC_DIR/"
echo "    installed $(find "$REPO_ROOT" -maxdepth 1 -name '*.md' | wc -l) markdown files"

UNIT_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
if [ ! -f "$UNIT_FILE" ]; then
  echo "==> Installing systemd unit (first time)"
  sudo cp "$REPO_ROOT/deploy/${SERVICE_NAME}.service" "$UNIT_FILE"
  sudo sed -i \
    -e "s|^EnvironmentFile=.*|EnvironmentFile=$ENV_FILE|" \
    -e "s|^ExecStart=.*|ExecStart=$BIN_PATH|" \
    "$UNIT_FILE"
  sudo systemctl daemon-reload
  sudo systemctl enable "$SERVICE_NAME"
  echo "    installed $UNIT_FILE"
fi

echo "==> Restarting $SERVICE_NAME"
sudo systemctl restart "$SERVICE_NAME"
sudo systemctl status "$SERVICE_NAME" --no-pager

# Wait for the unit to bind before verifying, so the check reports the real
# state rather than losing a race with startup.
for _ in $(seq 1 20); do
  systemctl is-active --quiet "$SERVICE_NAME" && break
  sleep 0.5
done

echo
echo "==> Verifying the update"
# New routes and tables are applied on startup, so a build that did not actually
# replace the binary — or a restart that silently reused the old one — looks
# healthy until somebody clicks a new page. This checks the deployed surface
# against what the current source registers.
if ! "$REPO_ROOT/scripts/verify-install.sh"; then
  echo
  echo "Update finished but verification failed — see above." >&2
  echo "If routes are missing, the service may still be running the old binary:" >&2
  echo "  sudo systemctl restart $SERVICE_NAME && ./scripts/verify-install.sh" >&2
  exit 1
fi

echo
echo "Update complete."