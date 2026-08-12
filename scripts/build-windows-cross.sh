#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GOCACHE_DIR="${GOCACHE_DIR:-/tmp/gocache-carelockconsulting}"
OUT_FILE="${1:-${ROOT_DIR}/dist/carelockconsulting-windows-amd64.exe}"

find_windows_toolchain_dir() {
  if [[ -n "${WINDOWS_TOOLCHAIN_DIR:-}" ]]; then
    printf '%s\n' "${WINDOWS_TOOLCHAIN_DIR}"
    return 0
  fi

  local candidate
  for candidate in "${ROOT_DIR}"/.toolchains/llvm-mingw-*; do
    if [[ -x "${candidate}/bin/x86_64-w64-mingw32-gcc" && -x "${candidate}/bin/x86_64-w64-mingw32-g++" ]]; then
      printf '%s\n' "${candidate}"
      return 0
    fi
  done

  return 1
}

TOOLCHAIN_DIR="$(find_windows_toolchain_dir || true)"
if [[ -n "${TOOLCHAIN_DIR}" ]]; then
  CC_DEFAULT="${TOOLCHAIN_DIR}/bin/x86_64-w64-mingw32-gcc"
  CXX_DEFAULT="${TOOLCHAIN_DIR}/bin/x86_64-w64-mingw32-g++"
elif command -v x86_64-w64-mingw32-gcc >/dev/null 2>&1 && command -v x86_64-w64-mingw32-g++ >/dev/null 2>&1; then
  CC_DEFAULT="$(command -v x86_64-w64-mingw32-gcc)"
  CXX_DEFAULT="$(command -v x86_64-w64-mingw32-g++)"
else
  echo "missing windows toolchain" >&2
  echo "install llvm-mingw under .toolchains/, set WINDOWS_TOOLCHAIN_DIR, or ensure x86_64-w64-mingw32-gcc/g++ are on PATH" >&2
  exit 1
fi

mkdir -p "${ROOT_DIR}/dist" "${GOCACHE_DIR}"

export CC="${WINDOWS_CC:-${CC_DEFAULT}}"
export CXX="${WINDOWS_CXX:-${CXX_DEFAULT}}"
export CGO_ENABLED=1
export GOOS=windows
export GOARCH=amd64
export GOCACHE="${GOCACHE_DIR}"

cd "${ROOT_DIR}"
go build -o "${OUT_FILE}" .
echo "built ${OUT_FILE}"
