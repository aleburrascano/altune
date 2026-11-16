#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
roots() {
  local files file
  files=$(git ls-files --cached --others --exclude-standard -- '*/module-spec.json') || return 1
  [ -n "$files" ] || return 0
  sort <<<"$files" | while IFS= read -r file; do dirname "$file"; done
}
if [ "${1:-}" = roots ]; then roots; exit; fi
kit=node_modules/.bin/module-spec
[ -x "$kit" ] || { echo "module-spec: not installed here. Run pnpm install at the repo root (it fetches the private kit from gitea, so it needs a gitea login)." >&2; exit 3; }
[ $# -gt 0 ] || { echo "usage: bash scripts/module-spec.sh <roots|check|baseline|api|affected|mutate|report> [flags]. Runs it in every code root that has a module-spec.json." >&2; exit 2; }

list=$(roots)
[ -n "$list" ] || { echo "module-spec: no module-spec.json in this repo, so '$1' has nothing to run on" >&2; exit 2; }
rc=0
while IFS= read -r root; do
  echo "== $root" >&2
  "$kit" "$@" --root "$root" || rc=1
done <<<"$list"
exit $rc
