#!/usr/bin/env bash
set -euo pipefail
base_ref=${1:?usage: kit-self-tests.sh <base-ref>. Runs the repo script self-tests that need the module-spec kit}
cd "$(git rev-parse --show-toplevel)"
base=$(git merge-base "$base_ref" HEAD) || { echo "kit-self-tests: no merge base with $base_ref" >&2; exit 3; }
changed=$(git diff --name-only "$base" HEAD)
if grep -qE '^scripts/(__tests__/)?violations(\.test)?\.(sh|mjs)$' <<<"$changed"; then
  bash scripts/__tests__/violations.test.sh
else
  echo "kit-self-tests: scripts/violations.* unchanged since $base_ref, nothing to run"
fi
