#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
kit=node_modules/.bin/module-spec
[ -x "$kit" ] || { echo "module-spec: not installed here. Run pnpm install at the repo root (it fetches the private kit from gitea, so it needs a gitea login)." >&2; exit 3; }
[ $# -gt 0 ] || { echo "usage: bash scripts/module-spec.sh <check|baseline|api|affected|mutate|report> [flags]. Runs it in every code root that has a module-spec.json." >&2; exit 2; }

rc=0
while read -r config; do
  root=$(dirname "$config")
  echo "== $root" >&2
  "$kit" "$@" --root "$root" || rc=1
done < <(git ls-files --cached --others --exclude-standard -- '*/module-spec.json' | sort)
exit $rc
