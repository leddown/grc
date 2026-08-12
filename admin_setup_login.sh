#!/usr/bin/env bash
# First-boot helper: starts grc with a one-time SETUP_TOKEN, bootstraps
# the first admin user, verifies login, then prints the command-line args /
# env vars to use for normal (post-bootstrap) startup. See RUNTIME_ARGS.md
# for the manual curl-based equivalent of this flow.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${ROOT_DIR}"

BINARY="${BINARY:-${ROOT_DIR}/grc}"
LISTEN_ADDR="${LISTEN_ADDR:-:8080}"
SQLITE_PATH="${SQLITE_PATH:-users.db}"
ALLOW_JSON_SAVE="${ALLOW_JSON_SAVE:-false}"
ADMIN_TOKEN="${ADMIN_TOKEN:-}"
SETUP_TOKEN="${SETUP_TOKEN:-}"
ADMIN_USERNAME="${ADMIN_USERNAME:-admin}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-}"
PID_FILE="${ROOT_DIR}/.admin_setup_login.pid"
LOG_FILE="${ROOT_DIR}/.admin_setup_login.log"
PRINT_ONLY=0
SKIP_BUILD=0

usage() {
  cat <<EOF
Usage: $(basename "$0") [options]

Starts grc, bootstraps the first admin user, verifies login, then
prints the command-line args / env vars for subsequent normal startups.

Options:
  -u, --username NAME       Admin username (default: admin)
  -p, --password PASS       Admin password (default: randomly generated)
  -l, --listen-addr ADDR    HTTP listen address (default: :8080)
  -d, --sqlite-path PATH    SQLite database path (default: users.db)
  -a, --admin-token TOKEN   Break-glass admin bearer token (default: none)
  -j, --allow-json-save     Enable JSON writeback endpoints
      --print-only          Print the env/args and exit; do not start anything
      --skip-build          Don't (re)build the binary even if missing
  -h, --help                Show this help

Environment variables of the same name (ADMIN_USERNAME, ADMIN_PASSWORD,
LISTEN_ADDR, SQLITE_PATH, ADMIN_TOKEN, SETUP_TOKEN, ALLOW_JSON_SAVE) are
honored as defaults and overridden by the flags above.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -u|--username) ADMIN_USERNAME="$2"; shift 2 ;;
    -p|--password) ADMIN_PASSWORD="$2"; shift 2 ;;
    -l|--listen-addr) LISTEN_ADDR="$2"; shift 2 ;;
    -d|--sqlite-path) SQLITE_PATH="$2"; shift 2 ;;
    -a|--admin-token) ADMIN_TOKEN="$2"; shift 2 ;;
    -j|--allow-json-save) ALLOW_JSON_SAVE="true"; shift ;;
    --print-only) PRINT_ONLY=1; shift ;;
    --skip-build) SKIP_BUILD=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 1 ;;
  esac
done

random_secret() {
  # 32 random bytes, base64url-encoded, no padding.
  openssl rand -base64 32 2>/dev/null | tr '+/' '-_' | tr -d '=\n' \
    || head -c 32 /dev/urandom | base64 | tr '+/' '-_' | tr -d '=\n'
}

if [[ -z "${SETUP_TOKEN}" ]]; then
  SETUP_TOKEN="$(random_secret)"
fi
GENERATED_PASSWORD=0
if [[ -z "${ADMIN_PASSWORD}" ]]; then
  ADMIN_PASSWORD="$(random_secret)"
  GENERATED_PASSWORD=1
fi

print_startup_args() {
  local note="$1"
  echo
  echo "=== ${note} ==="
  echo "LISTEN_ADDR='${LISTEN_ADDR}' SQLITE_PATH='${SQLITE_PATH}' ALLOW_JSON_SAVE=${ALLOW_JSON_SAVE}${ADMIN_TOKEN:+ ADMIN_TOKEN='${ADMIN_TOKEN}'} ${BINARY}"
  echo
  echo "  or with flags:"
  echo "${BINARY} -listen-addr='${LISTEN_ADDR}' -sqlite-path='${SQLITE_PATH}' -allow-json-save=${ALLOW_JSON_SAVE}${ADMIN_TOKEN:+ -admin-token='${ADMIN_TOKEN}'}"
}

if [[ "${PRINT_ONLY}" -eq 1 ]]; then
  print_startup_args "Normal startup (run any time, no setup token needed)"
  exit 0
fi

if [[ "${SKIP_BUILD}" -eq 0 ]]; then
  echo "building ${BINARY} from ./cmd/api..."
  go build -o "${BINARY}" ./cmd/api
elif [[ ! -x "${BINARY}" ]]; then
  echo "binary not found at ${BINARY} and --skip-build was set" >&2
  exit 1
fi

if [[ -f "${PID_FILE}" ]] && kill -0 "$(cat "${PID_FILE}")" 2>/dev/null; then
  echo "a server is already running with pid $(cat "${PID_FILE}") (per ${PID_FILE})" >&2
  echo "stop it first, or remove ${PID_FILE} if it is stale" >&2
  exit 1
fi

echo "starting ${BINARY} on ${LISTEN_ADDR} (db: ${SQLITE_PATH})..."
LISTEN_ADDR="${LISTEN_ADDR}" SQLITE_PATH="${SQLITE_PATH}" ALLOW_JSON_SAVE="${ALLOW_JSON_SAVE}" \
  ADMIN_TOKEN="${ADMIN_TOKEN}" SETUP_TOKEN="${SETUP_TOKEN}" \
  "${BINARY}" >"${LOG_FILE}" 2>&1 &
SERVER_PID=$!
echo "${SERVER_PID}" >"${PID_FILE}"

HOST_PORT="${LISTEN_ADDR#*:}"
BASE_URL="http://localhost:${HOST_PORT}"

echo "waiting for ${BASE_URL}/health..."
for _ in $(seq 1 30); do
  if curl -sS -o /dev/null "${BASE_URL}/health" 2>/dev/null; then
    break
  fi
  if ! kill -0 "${SERVER_PID}" 2>/dev/null; then
    echo "server exited early; see ${LOG_FILE}" >&2
    exit 1
  fi
  sleep 0.5
done

echo "bootstrapping admin user '${ADMIN_USERNAME}'..."
BOOTSTRAP_STATUS=$(curl -sS -o /tmp/admin_setup_login_bootstrap.$$ -w "%{http_code}" \
  -X POST "${BASE_URL}/auth/bootstrap-admin" \
  -H 'Content-Type: application/json' \
  -H "X-Setup-Token: ${SETUP_TOKEN}" \
  -d "{\"username\":\"${ADMIN_USERNAME}\",\"password\":\"${ADMIN_PASSWORD}\"}")
BOOTSTRAP_BODY="$(cat /tmp/admin_setup_login_bootstrap.$$)"
rm -f /tmp/admin_setup_login_bootstrap.$$

if [[ "${BOOTSTRAP_STATUS}" != "201" ]]; then
  echo "bootstrap failed (HTTP ${BOOTSTRAP_STATUS}): ${BOOTSTRAP_BODY}" >&2
  if [[ "${BOOTSTRAP_STATUS}" == "409" ]]; then
    echo "an admin already exists; this script only provisions the first admin." >&2
    echo "use the running server's /admin/user-management page (or /auth/users API) to manage users instead." >&2
  fi
  exit 1
fi
echo "admin user '${ADMIN_USERNAME}' created."

echo "verifying login..."
LOGIN_STATUS=$(curl -sS -o /dev/null -w "%{http_code}" \
  -X POST "${BASE_URL}/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"${ADMIN_USERNAME}\",\"password\":\"${ADMIN_PASSWORD}\"}")
if [[ "${LOGIN_STATUS}" != "200" ]]; then
  echo "login verification failed (HTTP ${LOGIN_STATUS})" >&2
  exit 1
fi
echo "login verified."

echo
echo "=== Setup complete ==="
echo "Server is running in the background (pid ${SERVER_PID}, logs: ${LOG_FILE})."
echo "Login page:        ${BASE_URL}/login"
echo "User management:   ${BASE_URL}/admin/user-management"
echo "Admin username:    ${ADMIN_USERNAME}"
if [[ "${GENERATED_PASSWORD}" -eq 1 ]]; then
  echo "Admin password:    ${ADMIN_PASSWORD}  (generated; store this securely, it is shown only once)"
else
  echo "Admin password:    (the one you provided)"
fi
echo "To stop the server: kill \$(cat ${PID_FILE})"

print_startup_args "Command-line args / env vars for future startups (SETUP_TOKEN no longer needed)"
