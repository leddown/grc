#!/usr/bin/env bash
# Build (unless --skip-build) and run grc using the active
# database profile from config/db.env (set via scripts/db-switch.sh):
#   local  -> embedded SQLite, LOCAL_MODE (no login)
#   remote -> external PostgreSQL, normal auth
#
# Usage: scripts/run-app.sh [--skip-build]
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

CONFIG="${ROOT_DIR}/config/db.env"
BINARY="${BINARY:-${ROOT_DIR}/grc}"
SKIP_BUILD=0
[[ "${1:-}" == "--skip-build" ]] && SKIP_BUILD=1

if [[ ! -f "$CONFIG" ]]; then
  echo "missing $CONFIG; run scripts/db-switch.sh local|remote first" >&2
  exit 1
fi
set -a
# shellcheck disable=SC1090
. "$CONFIG"
set +a
ACTIVE_BACKEND="${ACTIVE_BACKEND:-local}"
LISTEN_ADDR="${LISTEN_ADDR:-:8080}"

if [[ "$SKIP_BUILD" -eq 0 ]]; then
  echo "building $BINARY from ./cmd/api..."
  go build -o "$BINARY" ./cmd/api
elif [[ ! -x "$BINARY" ]]; then
  echo "binary not found at $BINARY and --skip-build was set" >&2
  exit 1
fi

case "$ACTIVE_BACKEND" in
  local)
    echo "starting LOCAL profile (SQLite ${LOCAL_SQLITE_PATH:-local.db}, local-mode=${LOCAL_MODE:-true}) on $LISTEN_ADDR"
    # Unset DATABASE_URL so the binary does not auto-select Postgres.
    exec env -u DATABASE_URL \
      LISTEN_ADDR="$LISTEN_ADDR" \
      SQLITE_PATH="${LOCAL_SQLITE_PATH:-local.db}" \
      LOCAL_MODE="${LOCAL_MODE:-true}" \
      "$BINARY"
    ;;
  remote)
    if [[ -z "${REMOTE_DATABASE_URL:-}" ]]; then
      echo "REMOTE_DATABASE_URL is empty in $CONFIG" >&2
      exit 1
    fi
    echo "starting REMOTE profile (PostgreSQL, auth) on $LISTEN_ADDR"
    # Unset SQLite/local-mode so only the Postgres backend with auth is used.
    exec env -u SQLITE_PATH -u LOCAL_MODE \
      LISTEN_ADDR="$LISTEN_ADDR" \
      DATABASE_URL="$REMOTE_DATABASE_URL" \
      "$BINARY"
    ;;
  *)
    echo "unknown ACTIVE_BACKEND '$ACTIVE_BACKEND' in $CONFIG (want local|remote)" >&2
    exit 1
    ;;
esac
