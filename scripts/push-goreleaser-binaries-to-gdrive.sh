#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${DIST_DIR:-${ROOT_DIR}/dist}"
GOOGLE_DRIVE_DESTINATION="${GOOGLE_DRIVE_DESTINATION:-}"
BUILD_SCRIPT="${ROOT_DIR}/scripts/build-goreleaser-binaries.sh"
BUILD_FIRST="${BUILD_FIRST:-0}"

if ! command -v rclone >/dev/null 2>&1; then
  echo "missing dependency: rclone" >&2
  echo "install and configure an rclone Google Drive remote, then rerun this script" >&2
  exit 1
fi

if [[ -z "${GOOGLE_DRIVE_DESTINATION}" ]]; then
  echo "missing Google Drive destination" >&2
  echo "set GOOGLE_DRIVE_DESTINATION to an rclone target such as gdrive:releases/grc" >&2
  exit 1
fi

if [[ "${BUILD_FIRST}" == "1" ]]; then
  if [[ ! -x "${BUILD_SCRIPT}" ]]; then
    echo "missing or non-executable build script: ${BUILD_SCRIPT}" >&2
    exit 1
  fi

  "${BUILD_SCRIPT}"
fi

if [[ ! -d "${DIST_DIR}" ]]; then
  echo "missing dist directory: ${DIST_DIR}" >&2
  echo "run scripts/build-goreleaser-binaries.sh or set BUILD_FIRST=1" >&2
  exit 1
fi

if [[ -z "$(find "${DIST_DIR}" -mindepth 1 -maxdepth 1 -print -quit)" ]]; then
  echo "dist directory is empty: ${DIST_DIR}" >&2
  echo "run scripts/build-goreleaser-binaries.sh or set BUILD_FIRST=1" >&2
  exit 1
fi

echo "uploading ${DIST_DIR} contents to ${GOOGLE_DRIVE_DESTINATION}"

# Pass through extra rclone flags, e.g. --dry-run or --progress.
rclone copy "${DIST_DIR}" "${GOOGLE_DRIVE_DESTINATION}" "$@"

echo "uploaded binaries to ${GOOGLE_DRIVE_DESTINATION}"
