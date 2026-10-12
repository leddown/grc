#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# A cache that outlives one run. It matters less than it did — nothing here
# compiles C any more — but a warm cache is still the difference between a
# four-second build and a thirty-second one.
GOCACHE_DIR="${GOCACHE_DIR:-${XDG_CACHE_HOME:-${HOME}/.cache}/gocache-grc}"
OUT_FILE="${1:-${ROOT_DIR}/dist/grc-windows-amd64.exe}"

mkdir -p "${ROOT_DIR}/dist" "${GOCACHE_DIR}"

# No toolchain, no CC, no CGO. The SQLite driver is pure Go, so a Windows binary
# is built here the same way a Linux one is: set GOOS and compile. This script
# used to require llvm-mingw under .toolchains/ and refuse to run without it,
# because the C driver needed a Windows C compiler to build against.
export CGO_ENABLED=0
export GOOS=windows
export GOARCH=amd64
export GOCACHE="${GOCACHE_DIR}"

cd "${ROOT_DIR}"
go build -o "${OUT_FILE}" .
echo "built ${OUT_FILE}"
