#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG_FILE="${GORELEASER_CONFIG:-${ROOT_DIR}/.goreleaser.yaml}"
DIST_DIR="${DIST_DIR:-${ROOT_DIR}/dist}"
# A cache that outlives one run. /tmp was the previous default and is cleared on
# reboot, so every build after a restart started cold — which used to mean
# recompiling the SQLite amalgamation once per target.
GOCACHE_DIR="${GOCACHE_DIR:-${XDG_CACHE_HOME:-${HOME}/.cache}/gocache-grc}"
SMOKE_TEST_SCRIPT="${ROOT_DIR}/scripts/smoke-test-goreleaser-binaries.sh"
DATA_FILES=(
  "internal/data/merged_nist_controls_master_replaced_from_controls_all.json"
  "internal/data/NFR_incremental_keys_with_domain.json"
)

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

find_darwin_toolchain_bin_dir() {
  if [[ -n "${DARWIN_TOOLCHAIN_BIN_DIR:-}" ]]; then
    printf '%s\n' "${DARWIN_TOOLCHAIN_BIN_DIR}"
    return 0
  fi

  local candidate
  for candidate in "${ROOT_DIR}"/.toolchains/osxcross-*/target/bin; do
    if [[ -x "${candidate}/o64-clang" && -x "${candidate}/o64-clang++" ]]; then
      printf '%s\n' "${candidate}"
      return 0
    fi
  done

  return 1
}
if ! command -v goreleaser >/dev/null 2>&1; then
  echo "missing dependency: goreleaser" >&2
  echo "install goreleaser and rerun this script" >&2
  exit 1
fi

if [[ ! -f "${CONFIG_FILE}" ]]; then
  echo "missing Goreleaser config: ${CONFIG_FILE}" >&2
  exit 1
fi
if [[ ! -x "${SMOKE_TEST_SCRIPT}" ]]; then
  echo "missing or non-executable smoke test script: ${SMOKE_TEST_SCRIPT}" >&2
  exit 1
fi

# The cross toolchains are optional now. Nothing in this module uses cgo — the
# SQLite driver is pure Go — so a windows or darwin build needs no C compiler.
# They are still discovered and exported, because a .goreleaser.yaml that sets
# CGO_ENABLED=1 and templates {{ .Env.WINDOWS_CC }} would fail on an unset
# variable; a config with CGO_ENABLED=0 ignores them. Missing toolchains are a
# warning rather than the hard failure they used to be, so a machine with none
# can still cut a release.
WINDOWS_CC_DEFAULT=""
WINDOWS_CXX_DEFAULT=""
WINDOWS_TOOLCHAIN_DIR="$(find_windows_toolchain_dir || true)"
if [[ -n "${WINDOWS_TOOLCHAIN_DIR}" ]]; then
  WINDOWS_CC_DEFAULT="${WINDOWS_TOOLCHAIN_DIR}/bin/x86_64-w64-mingw32-gcc"
  WINDOWS_CXX_DEFAULT="${WINDOWS_TOOLCHAIN_DIR}/bin/x86_64-w64-mingw32-g++"
elif command -v x86_64-w64-mingw32-gcc >/dev/null 2>&1 && command -v x86_64-w64-mingw32-g++ >/dev/null 2>&1; then
  WINDOWS_CC_DEFAULT="$(command -v x86_64-w64-mingw32-gcc)"
  WINDOWS_CXX_DEFAULT="$(command -v x86_64-w64-mingw32-g++)"
else
  echo "note: no windows C toolchain found; fine unless the config sets CGO_ENABLED=1" >&2
fi

DARWIN_CC_DEFAULT=""
DARWIN_CXX_DEFAULT=""
DARWIN_TOOLCHAIN_BIN_DIR="$(find_darwin_toolchain_bin_dir || true)"
if [[ -n "${DARWIN_TOOLCHAIN_BIN_DIR}" ]]; then
  DARWIN_CC_DEFAULT="${DARWIN_TOOLCHAIN_BIN_DIR}/o64-clang"
  DARWIN_CXX_DEFAULT="${DARWIN_TOOLCHAIN_BIN_DIR}/o64-clang++"
elif command -v o64-clang >/dev/null 2>&1 && command -v o64-clang++ >/dev/null 2>&1; then
  DARWIN_CC_DEFAULT="$(command -v o64-clang)"
  DARWIN_CXX_DEFAULT="$(command -v o64-clang++)"
else
  echo "note: no darwin C toolchain found; fine unless the config sets CGO_ENABLED=1" >&2
fi
mkdir -p "${DIST_DIR}" "${GOCACHE_DIR}"

export GOCACHE="${GOCACHE_DIR}"
export WINDOWS_CC="${WINDOWS_CC:-${WINDOWS_CC_DEFAULT}}"
export WINDOWS_CXX="${WINDOWS_CXX:-${WINDOWS_CXX_DEFAULT}}"
export DARWIN_CC="${DARWIN_CC:-${DARWIN_CC_DEFAULT}}"
export DARWIN_CXX="${DARWIN_CXX:-${DARWIN_CXX_DEFAULT}}"

cd "${ROOT_DIR}"
goreleaser build --snapshot --clean --config "${CONFIG_FILE}"

find "${DIST_DIR}" -mindepth 1 -maxdepth 1 -type d | while read -r artifact_dir; do
  mkdir -p "${artifact_dir}/internal/data"
  for data_file in "${DATA_FILES[@]}"; do
    cp "${ROOT_DIR}/${data_file}" "${artifact_dir}/${data_file}"
  done
done

"${SMOKE_TEST_SCRIPT}"

echo "built Goreleaser artifacts into ${DIST_DIR}"
