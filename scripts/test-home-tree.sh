#!/usr/bin/env bash
set -euo pipefail

dir=${1:?usage: test-home-tree.sh DIR}
here=$(cd "$(dirname "$0")" && pwd)
root_commit=$(git rev-list --max-parents=0 HEAD | tail -n 1)

status=0
out=$(node "$here/test-home.mjs" "$root_commit" 2>&1) || status=$?
if [ "$status" = 3 ]; then
  printf '%s\n' "$out"
  exit 3
fi

found=$(awk -v prefix="${dir%/}/" 'index($0, prefix) == 1' <<<"$out")
if [ -n "$found" ]; then
  printf '%s\n' "$found"
  echo "test-home-tree: test files under $dir that are not their unit's first test file; rename or merge them into the unit's test file"
  exit 1
fi
if [ "$status" != 0 ] && [ "$status" != 1 ]; then
  printf '%s\n' "$out"
  echo "test-home-tree: test-home exited $status"
  exit "$status"
fi
echo "test-home-tree: every test file under $dir lives with its unit"
