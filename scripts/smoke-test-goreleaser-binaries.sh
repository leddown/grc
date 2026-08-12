#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${DIST_DIR:-${ROOT_DIR}/dist}"
BINARY_BASENAME="${BINARY_BASENAME:-grc}"
DATA_FILES=(
  "internal/data/merged_nist_controls_master_replaced_from_controls_all.json"
  "internal/data/NFR_incremental_keys_with_domain.json"
)

linux_binary="$(find "${DIST_DIR}" -type f -name "${BINARY_BASENAME}" -path "*linux*" -print | head -n 1)"
darwin_binary="$(find "${DIST_DIR}" -type f -name "${BINARY_BASENAME}" -path "*darwin*" -print | head -n 1)"
windows_binary="$(find "${DIST_DIR}" -type f -name "${BINARY_BASENAME}.exe" -path "*windows*" -print | head -n 1)"

if [[ -z "${linux_binary}" ]]; then
  echo "missing linux artifact under ${DIST_DIR}" >&2
  exit 1
fi
if [[ -z "${darwin_binary}" ]]; then
  echo "missing darwin artifact under ${DIST_DIR}" >&2
  exit 1
fi
if [[ -z "${windows_binary}" ]]; then
  echo "missing windows artifact under ${DIST_DIR}" >&2
  exit 1
fi

echo "found linux artifact: ${linux_binary}"
echo "found darwin artifact: ${darwin_binary}"
echo "found windows artifact: ${windows_binary}"

for artifact in "${linux_binary}" "${darwin_binary}" "${windows_binary}"; do
  artifact_dir="$(dirname "${artifact}")"
  for data_file in "${DATA_FILES[@]}"; do
    if [[ ! -f "${artifact_dir}/${data_file}" ]]; then
      echo "missing data file for artifact ${artifact}: ${artifact_dir}/${data_file}" >&2
      exit 1
    fi
  done
done

case "$(uname -s)" in
  Linux*)
    echo "running smoke test: ${linux_binary} --help"
    "${linux_binary}" --help >/dev/null
    ;;
  Darwin*)
    echo "running smoke test: ${darwin_binary} --help"
    "${darwin_binary}" --help >/dev/null
    ;;
  MINGW*|MSYS*|CYGWIN*)
    echo "running smoke test: ${windows_binary} --help"
    "${windows_binary}" --help >/dev/null
    ;;
  *)
    echo "unknown host OS; skipping executable smoke run" >&2
    ;;
esac

echo "goreleaser smoke test passed"
