#!/usr/bin/env bash
set -euo pipefail

[ $# -ge 1 ] || { echo "usage: test-home-tree.sh DIR..." >&2; exit 2; }
here=$(cd "$(dirname "$0")" && pwd)
bash "$here/history-complete.sh" || exit 3
root_commit=$(git rev-list --max-parents=0 HEAD | tail -n 1)

status=0
out=$(node "$here/test-home.mjs" "$root_commit" 2>&1) || status=$?
if [ "$status" = 3 ]; then
  printf '%s\n' "$out"
  exit 3
fi

found=
for dir in "$@"; do
  found+=$(awk -v prefix="${dir%/}/" 'index($0, prefix) == 1' <<<"$out")$'\n'
done
found=${found//$'\n\n'/$'\n'}
if [ -n "${found//[$'\n']/}" ]; then
  printf '%s' "$found"
  echo "test-home-tree: test files under $* that do not live with their unit; name each for the source file it tests, or merge it into that unit's first test file"
  exit 1
fi
if [ "$status" != 0 ] && [ "$status" != 1 ]; then
  printf '%s\n' "$out"
  echo "test-home-tree: test-home exited $status"
  exit "$status"
fi
echo "test-home-tree: every test file under $* lives with its unit"
