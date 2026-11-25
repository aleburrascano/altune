#!/usr/bin/env bash
set -euo pipefail
usage="usage: go-deadcode.sh   from any directory; fails when deadcode -test ./... over services/go-api reports any function no main or test reaches"
[ $# = 0 ] || { echo "$usage" >&2; exit 2; }
module=$(cd "$(dirname "$0")/../../services/go-api" && pwd)
[ -f "$module/go.mod" ] || { echo "go-deadcode: $module/go.mod is missing, so nothing was checked" >&2; exit 2; }
cd "$module"
deadcode=golang.org/x/tools/cmd/deadcode@v0.53.0
if ! found=$(go run "$deadcode" -test ./...); then
  echo "go-deadcode: go run $deadcode failed, so nothing was checked" >&2
  exit 2
fi
if [ -n "$found" ]; then
  printf '%s\n' "$found" >&2
  echo "go-deadcode: the functions above are unreachable from every main and every test; delete them, or call them from code that ships" >&2
  exit 1
fi
