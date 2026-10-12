#!/usr/bin/env bash
# Build the Policy Studio editor bundle (web/policy-studio) into
# internal/policystudio/assets, and regenerate the cross-language fixtures.
#
#   scripts/build-policy-studio.sh           build
#   scripts/build-policy-studio.sh --check   build into a scratch copy and fail
#                                            if it differs from what is committed
#
# Node is a development-time dependency only: the bundle is committed, so
# `go build` works from a clean checkout without it. Dependencies install with
# --ignore-scripts from the committed lockfile, and the y-tiptap node-marks
# patch is applied on top (see POLICY_STUDIO.md §3.3).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WEB="$REPO_ROOT/web/policy-studio"
REQUIRED_NODE_MAJOR=24

if ! command -v node >/dev/null 2>&1; then
  echo "node is not on PATH; install Node $REQUIRED_NODE_MAJOR LTS" >&2
  exit 1
fi
NODE_MAJOR="$(node -p 'process.versions.node.split(".")[0]')"
if [ "$NODE_MAJOR" != "$REQUIRED_NODE_MAJOR" ]; then
  echo "Node $REQUIRED_NODE_MAJOR LTS is required (found $(node --version)); the bundle is only reproducible on the pinned major" >&2
  exit 1
fi

cd "$WEB"
npm ci --ignore-scripts --no-audit --no-fund
node patches/apply-y-tiptap-node-marks.mjs
node fixtures/gen.mjs gen

if [ "${1:-}" = "--check" ]; then
  before="$(cd "$REPO_ROOT" && git status --porcelain -- internal/policystudio/assets THIRD_PARTY_NOTICES.md web/policy-studio/schema.snapshot.json web/policy-studio/fixtures/cases)"
  node build.mjs
  after="$(cd "$REPO_ROOT" && git status --porcelain -- internal/policystudio/assets THIRD_PARTY_NOTICES.md web/policy-studio/schema.snapshot.json web/policy-studio/fixtures/cases)"
  if [ "$before" != "$after" ] || [ -n "$after" ]; then
    echo "the rebuilt bundle or fixtures differ from what is committed:" >&2
    (cd "$REPO_ROOT" && git status --short -- internal/policystudio/assets THIRD_PARTY_NOTICES.md web/policy-studio)
    exit 1
  fi
  echo "bundle reproduces the committed build"
  exit 0
fi

node build.mjs

# The Go seeder's output must be what the editor verified: regenerate both.
cd "$REPO_ROOT"
go test ./internal/policystudio/ -run TestSeededDocumentsAreTheOnesTheEditorVerified -update >/dev/null
(cd "$WEB" && node fixtures/gen.mjs verify)
go test ./internal/policystudio/ >/dev/null
echo "policy studio bundle built"
