#!/usr/bin/env bash
set -euo pipefail
dir=$1
main=/home/ubuntu/projects/altune/$dir
cd "$WT/$dir"
[ -e node_modules ] && exit 0
same_tree() { cmp -s <(jq -S 'del(.packages[""])' package-lock.json) <(jq -S 'del(.packages[""])' "$main/package-lock.json"); }
if [ -d "$main/node_modules" ] && same_tree; then
  ln -s "$main/node_modules" node_modules
  echo "$dir: linked node_modules from the main checkout"
else
  npm ci --no-audit --no-fund
fi
