#!/usr/bin/env bash
# Switch the active database profile used by scripts/run-app.sh between the
# local SQLite profile and the remote PostgreSQL profile. State lives in
# config/db.env (created from config/db.env.example on first use).
#
# Usage:
#   scripts/db-switch.sh local     # use embedded SQLite (laptop) profile
#   scripts/db-switch.sh remote    # use external PostgreSQL profile
#   scripts/db-switch.sh status    # show the active profile and endpoints
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG="${ROOT_DIR}/config/db.env"
EXAMPLE="${ROOT_DIR}/config/db.env.example"

ensure_config() {
  if [[ ! -f "$CONFIG" ]]; then
    cp "$EXAMPLE" "$CONFIG"
    echo "created $CONFIG from example -- edit REMOTE_DATABASE_URL before using the remote profile"
  fi
}

set_active() {
  ensure_config
  local val="$1"
  if grep -q '^ACTIVE_BACKEND=' "$CONFIG"; then
    sed -i "s|^ACTIVE_BACKEND=.*|ACTIVE_BACKEND=${val}|" "$CONFIG"
  else
    printf 'ACTIVE_BACKEND=%s\n' "$val" >>"$CONFIG"
  fi
  echo "active backend set to: $val"
}

mask_url() {
  # Hide the password in a postgres:// URL when printing.
  printf '%s' "$1" | sed -E 's#(://[^:/@]+:)[^@]*@#\1***@#'
}

status() {
  ensure_config
  set -a
  # shellcheck disable=SC1090
  . "$CONFIG"
  set +a
  echo "active backend : ${ACTIVE_BACKEND:-local}"
  echo "local sqlite   : ${LOCAL_SQLITE_PATH:-local.db} (local-mode=${LOCAL_MODE:-true})"
  if [[ -n "${REMOTE_DATABASE_URL:-}" ]]; then
    echo "remote pg      : $(mask_url "$REMOTE_DATABASE_URL")"
  else
    echo "remote pg      : (unset)"
  fi
}

case "${1:-status}" in
  local) set_active local ;;
  remote) set_active remote ;;
  status) status ;;
  *)
    echo "usage: $(basename "$0") {local|remote|status}" >&2
    exit 1
    ;;
esac
