#!/usr/bin/env bash
# Create an auth user or reset an existing user's password, working directly on
# the database — no running server, no admin session, no SETUP_TOKEN needed.
# (admin_setup_login.sh only ever provisions the *first* admin; this script
# handles every user after that, and password resets when nobody can log in.)
#
# Every run prints the repo directory and the resolved database to stderr, so
# an unexpected "auth user not found" shows which database was actually opened.
#
# Usage:
#   scripts/manage-user.sh list                      show every account + the db in use
#   scripts/manage-user.sh create -u alice [--admin] [--pages /reports,/crm/*]
#   scripts/manage-user.sh passwd -u alice [--revoke]
#   scripts/manage-user.sh set-admin -u alice [--remove]
#   scripts/manage-user.sh delete -u alice
#
# set-admin is the recovery path when the only account is not an admin: in
# single-user mode 'create' cannot add a second account, and every in-app route
# to admin is itself behind an admin session. It needs no password.
#
# Password input (create/passwd), in order of precedence:
#   --generate         generate a strong random password and print it once
#   --password-stdin   read the password from this script's stdin
#   (default)          prompt twice, with no echo
#
# Backend selection, in order of precedence:
#   --db SPEC          postgres:// URL or SQLite file path
#   $DATABASE_URL / $SQLITE_PATH
#   config/db.env      the active profile set by scripts/db-switch.sh
#   users.db
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

CONFIG="${ROOT_DIR}/config/db.env"
# Keep in sync with minPasswordLength in internal/authn/service.go.
MIN_PASSWORD_LENGTH=5
DB_SPEC=""
COMMAND=""
USERNAME=""
PAGES=""
IS_ADMIN=0
REMOVE_ADMIN=0
REVOKE=0
GENERATE=0
PASSWORD_STDIN=0

# Prints the header comment as help, stopping at the first non-comment line, so
# editing the block above cannot silently truncate or overrun the help text the
# way a hardcoded line range did.
usage() { awk 'NR>1 && /^#/ { sub(/^# ?/, ""); print; next } NR>1 { exit }' "${BASH_SOURCE[0]}"; }

if [[ $# -eq 0 ]]; then
  usage >&2
  exit 1
fi

COMMAND="$1"
shift
while [[ $# -gt 0 ]]; do
  case "$1" in
    -u|--user|--username) USERNAME="$2"; shift 2 ;;
    --db) DB_SPEC="$2"; shift 2 ;;
    --pages) PAGES="$2"; shift 2 ;;
    --admin) IS_ADMIN=1; shift ;;
    --remove) REMOVE_ADMIN=1; shift ;;
    --revoke) REVOKE=1; shift ;;
    --generate) GENERATE=1; shift ;;
    --password-stdin) PASSWORD_STDIN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 1 ;;
  esac
done

case "$COMMAND" in
  list|create|passwd|set-password|set-admin|delete) ;;
  -h|--help) usage; exit 0 ;;
  *) echo "unknown command: $COMMAND" >&2; usage >&2; exit 1 ;;
esac

# Record where the backend came from, so a surprising "user not found" shows
# which link in the precedence chain won (sudo drops DATABASE_URL/SQLITE_PATH).
if [[ -n "$DB_SPEC" ]]; then
  DB_SOURCE="--db flag"
elif [[ -n "${DATABASE_URL:-}" ]]; then
  DB_SOURCE="\$DATABASE_URL"
elif [[ -n "${SQLITE_PATH:-}" ]]; then
  DB_SOURCE="\$SQLITE_PATH"
elif [[ -f "$CONFIG" ]]; then
  DB_SOURCE="config/db.env profile"
else
  DB_SOURCE="built-in default (users.db)"
fi

# Fall back to the active db.env profile when no explicit backend was given.
if [[ -z "$DB_SPEC" && -z "${DATABASE_URL:-}" && -z "${SQLITE_PATH:-}" && -f "$CONFIG" ]]; then
  # shellcheck disable=SC1090
  . "$CONFIG"
  case "${ACTIVE_BACKEND:-local}" in
    remote) DB_SPEC="${REMOTE_DATABASE_URL:-}" ;;
    *) DB_SPEC="${LOCAL_SQLITE_PATH:-local.db}" ;;
  esac
fi

echo "manage-user: repo:     ${ROOT_DIR}" >&2
echo "manage-user: backend:  from ${DB_SOURCE}" >&2

USERCTL_BIN="${USERCTL_BIN:-}"
run_userctl() {
  local args=()
  [[ -n "$DB_SPEC" ]] && args+=(-db "$DB_SPEC")
  args+=("$@")
  if [[ -n "$USERCTL_BIN" ]]; then
    "$USERCTL_BIN" "${args[@]}"
  else
    go run ./cmd/userctl "${args[@]}"
  fi
}

if [[ "$COMMAND" == "list" ]]; then
  run_userctl list
  exit 0
fi

if [[ -z "$USERNAME" ]]; then
  echo "$COMMAND requires -u USERNAME" >&2
  exit 1
fi

# Before the password block below: changing the admin flag needs no password,
# and prompting for one would be a confusing barrier on the exact command an
# operator reaches for when they are already locked out.
if [[ "$COMMAND" == "set-admin" ]]; then
  SET_ADMIN_ARGS=(set-admin -u "$USERNAME")
  [[ "$REMOVE_ADMIN" -eq 1 ]] && SET_ADMIN_ARGS+=(-remove)
  run_userctl "${SET_ADMIN_ARGS[@]}"
  exit 0
fi

if [[ "$COMMAND" == "delete" ]]; then
  read -r -p "delete auth user '${USERNAME}'? [y/N] " confirm
  [[ "$confirm" == [yY] ]] || { echo "aborted"; exit 1; }
  run_userctl delete -u "$USERNAME"
  exit 0
fi

# create / passwd: obtain the password without ever putting it on a command line.
PASSWORD=""
if [[ "$GENERATE" -eq 1 ]]; then
  PASSWORD="$(openssl rand -base64 24 2>/dev/null | tr '+/' '-_' | tr -d '=\n' \
    || head -c 24 /dev/urandom | base64 | tr '+/' '-_' | tr -d '=\n')"
elif [[ "$PASSWORD_STDIN" -eq 1 ]]; then
  IFS= read -r PASSWORD || true
else
  read -r -s -p "password for '${USERNAME}' (min ${MIN_PASSWORD_LENGTH} chars): " PASSWORD; echo
  read -r -s -p "confirm password: " CONFIRM; echo
  if [[ "$PASSWORD" != "$CONFIRM" ]]; then
    echo "passwords do not match" >&2
    exit 1
  fi
fi

if [[ "${#PASSWORD}" -lt "$MIN_PASSWORD_LENGTH" ]]; then
  echo "password must be at least ${MIN_PASSWORD_LENGTH} characters" >&2
  exit 1
fi

case "$COMMAND" in
  create)
    CREATE_ARGS=(create -u "$USERNAME")
    [[ "$IS_ADMIN" -eq 1 ]] && CREATE_ARGS+=(-admin)
    [[ -n "$PAGES" ]] && CREATE_ARGS+=(-pages "$PAGES")
    printf '%s' "$PASSWORD" | run_userctl "${CREATE_ARGS[@]}"
    ;;
  passwd|set-password)
    PASSWD_ARGS=(set-password -u "$USERNAME")
    [[ "$REVOKE" -eq 1 ]] && PASSWD_ARGS+=(-revoke)
    printf '%s' "$PASSWORD" | run_userctl "${PASSWD_ARGS[@]}"
    ;;
esac

if [[ "$GENERATE" -eq 1 ]]; then
  echo "generated password: ${PASSWORD}  (shown only once — store it securely)"
fi
