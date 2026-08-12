#!/usr/bin/env bash
# Runs carelockconsulting in local mode: no login, no user management, every page and
# admin action open. Intended for single-user laptop use only — see
# RUNTIME_ARGS.md ("Local (no-login) laptop mode").
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${ROOT_DIR}"

BINARY="${BINARY:-${ROOT_DIR}/carelockconsulting}"
LISTEN_ADDR="${LISTEN_ADDR:-:8080}"
SQLITE_PATH="${SQLITE_PATH:-local.db}"
SKIP_BUILD=0

usage() {
  cat <<EOF
Usage: $(basename "$0") [options]

Builds (unless --skip-build) and runs carelockconsulting with LOCAL_MODE=true: no
login page, no user management, every page and admin action open.

Options:
  -l, --listen-addr ADDR    HTTP listen address (default: :8080)
  -d, --sqlite-path PATH    SQLite database path (default: local.db)
      --skip-build          Don't (re)build the binary even if missing
  -h, --help                Show this help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -l|--listen-addr) LISTEN_ADDR="$2"; shift 2 ;;
    -d|--sqlite-path) SQLITE_PATH="$2"; shift 2 ;;
    --skip-build) SKIP_BUILD=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 1 ;;
  esac
done

if [[ "${SKIP_BUILD}" -eq 0 ]]; then
  echo "building ${BINARY} from ./cmd/api..."
  go build -o "${BINARY}" ./cmd/api
elif [[ ! -x "${BINARY}" ]]; then
  echo "binary not found at ${BINARY} and --skip-build was set" >&2
  exit 1
fi

echo "starting ${BINARY} on ${LISTEN_ADDR} (db: ${SQLITE_PATH}, local mode: no login)..."
exec env LISTEN_ADDR="${LISTEN_ADDR}" SQLITE_PATH="${SQLITE_PATH}" LOCAL_MODE=true "${BINARY}"
