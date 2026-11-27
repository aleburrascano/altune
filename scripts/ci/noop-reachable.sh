#!/usr/bin/env bash
set -euo pipefail
usage="usage: noop-reachable.sh [DIR]
  Fails when a Noop constructor (func Noop*) declared in a tracked non-test Go file under DIR (default: services/go-api/internal)
  is used by no other tracked non-test Go file. A door file (door_*.go, *_door.go) only re-exports a name, so it is not a use.
  A Noop that only tests use belongs in a test-support package or a _test.go file, and a Noop nobody uses is dead.
  Untracked files are not read, so git add a new file first.
  Exit 1 lists each unused Noop, and exit 2 means the check could not run."
case "${1:-}" in
  -h | --help)
    echo "$usage"
    exit 0
    ;;
esac
[ $# -le 1 ] || {
  echo "$usage" >&2
  exit 2
}
root=$(git rev-parse --show-toplevel)
cd "$root"
dir=${1:-services/go-api/internal}
[ -d "$dir" ] || {
  echo "noop-reachable: $dir is not a directory, so nothing was checked" >&2
  exit 2
}
dir=$(realpath --relative-to="$root" "$dir")

declared=$(git grep -nE '^func Noop[A-Za-z0-9_]*\(' -- "$dir/*.go" ':!*_test.go' || true)
status=0
while IFS= read -r line; do
  [ -n "$line" ] || continue
  file=${line%%:*}
  name=$(sed -E 's/^[^:]*:[0-9]+:func (Noop[A-Za-z0-9_]*)\(.*/\1/' <<<"$line")
  users=$(git grep -lw "$name" -- "$dir/*.go" ':!*_test.go' || true)
  used=$(grep -vxF "$file" <<<"$users" | grep -vE '(^|/)(door_[^/]*|[^/]*_door)\.go$' || true)
  if [ -z "$used" ]; then
    echo "noop-reachable: $name ($file) is used by no production code; delete it, or move the double into a test-support package" >&2
    status=1
  fi
done <<<"$declared"
exit "$status"
