#!/usr/bin/env bash
# Export local data up to the remote PostgreSQL database, or pull remote data
# down into the local SQLite database, using the application's built-in sync
# mode (upsert/merge by each table's natural key; rows present only on the
# destination are left untouched, except the derived link table which is
# rebuilt). Endpoints come from config/db.env (LOCAL_SQLITE_PATH /
# REMOTE_DATABASE_URL).
#
# Usage:
#   scripts/db-sync.sh push [--yes] [--skip-build]   # local  -> remote
#   scripts/db-sync.sh pull [--yes] [--skip-build]   # remote -> local
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

CONFIG="${ROOT_DIR}/config/db.env"
BINARY="${BINARY:-${ROOT_DIR}/grc}"

MODE="${1:-}"
shift || true
ASSUME_YES=0
SKIP_BUILD=0
for arg in "$@"; do
  case "$arg" in
    --yes|-y) ASSUME_YES=1 ;;
    --skip-build) SKIP_BUILD=1 ;;
    *) echo "unknown argument: $arg" >&2; exit 1 ;;
  esac
done

if [[ "$MODE" != "push" && "$MODE" != "pull" ]]; then
  echo "usage: $(basename "$0") {push|pull} [--yes] [--skip-build]" >&2
  exit 1
fi

if [[ ! -f "$CONFIG" ]]; then
  echo "missing $CONFIG; run scripts/db-switch.sh first and set REMOTE_DATABASE_URL" >&2
  exit 1
fi
set -a
# shellcheck disable=SC1090
. "$CONFIG"
set +a

LOCAL_SQLITE_PATH="${LOCAL_SQLITE_PATH:-local.db}"
if [[ -z "${REMOTE_DATABASE_URL:-}" ]]; then
  echo "REMOTE_DATABASE_URL is empty in $CONFIG" >&2
  exit 1
fi

mask_url() { printf '%s' "$1" | sed -E 's#(://[^:/@]+:)[^@]*@#\1***@#'; }

if [[ "$MODE" == "push" ]]; then
  echo "PUSH: $LOCAL_SQLITE_PATH  ->  $(mask_url "$REMOTE_DATABASE_URL")"
  echo "      (inserts new rows and updates matching ones on the remote)"
else
  echo "PULL: $(mask_url "$REMOTE_DATABASE_URL")  ->  $LOCAL_SQLITE_PATH"
  echo "      (inserts new rows and updates matching ones in the local DB)"
fi

if [[ "$ASSUME_YES" -ne 1 ]]; then
  read -r -p "Proceed? [y/N] " reply
  case "$reply" in
    y|Y|yes|YES) ;;
    *) echo "aborted."; exit 0 ;;
  esac
fi

if [[ "$SKIP_BUILD" -eq 0 ]]; then
  echo "building $BINARY from ./cmd/api..."
  go build -o "$BINARY" ./cmd/api
elif [[ ! -x "$BINARY" ]]; then
  echo "binary not found at $BINARY and --skip-build was set" >&2
  exit 1
fi

# The active backend for the sync run is the local SQLite DB; the remote URL is
# the other side. db.Open() in the binary detects the engine from the spec.
if [[ "$MODE" == "push" ]]; then
  exec env -u DATABASE_URL \
    SQLITE_PATH="$LOCAL_SQLITE_PATH" \
    "$BINARY" -sync-to "$REMOTE_DATABASE_URL"
else
  exec env -u DATABASE_URL \
    SQLITE_PATH="$LOCAL_SQLITE_PATH" \
    "$BINARY" -sync-from "$REMOTE_DATABASE_URL"
fi
