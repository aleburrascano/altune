#!/usr/bin/env bash
set -euo pipefail
usage="usage: go-deadcode.sh   run in a Go module directory; fails when deadcode -test ./... reports any function no main or test reaches"
[ $# = 0 ] || { echo "$usage" >&2; exit 2; }
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
