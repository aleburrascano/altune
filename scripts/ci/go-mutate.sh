#!/usr/bin/env bash
set -euo pipefail
usage="usage: go-mutate.sh BASE_REF MODULE_DIR   mutation-tests the Go lines MODULE_DIR changed since BASE_REF, package by package, and fails on any mutant a test does not catch"
[ $# = 2 ] || { echo "$usage" >&2; exit 2; }
base=$1
dir=$2
gremlins=github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0

cd "$dir"
module_prefix=$(git rev-parse --show-prefix)
mapfile -t packages < <(git diff --name-only --relative --merge-base "$base" -- . | grep -E '\.go$' | grep -vE '_test\.go$' | xargs -r -n1 dirname | sort -u)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

real_git=$(command -v git)
mkdir "$work/bin"
printf '#!/bin/sh\nif [ "$1" = diff ]; then shift; exec "%s" diff --relative="$GO_MUTATE_PACKAGE" "$@"; fi\nexec "%s" "$@"\n' "$real_git" "$real_git" >"$work/bin/git"
chmod +x "$work/bin/git"

killed=0
survivors=()
for package in "${packages[@]}"; do
  [ -d "$package" ] || continue
  report=$work/report.json
  rm -f "$report"
  if ! out=$(env GOFLAGS="${GOFLAGS:+$GOFLAGS }-count=1" PATH="$work/bin:$PATH" GO_MUTATE_PACKAGE="$module_prefix$package/" go run "$gremlins" unleash --diff "$base" --output "$report" "./$package" 2>&1); then
    echo "$out" >&2
    echo "go-mutate: gremlins failed in $dir/$package (a failing test or build stops it)" >&2
    exit 1
  fi
  if [ ! -f "$report" ]; then
    grep -q 'No results to report.' <<<"$out" && continue
    echo "$out" >&2
    echo "go-mutate: gremlins wrote no report for $dir/$package and did not say why" >&2
    exit 1
  fi
  killed=$((killed + $(jq '[.files[].mutations[] | select(.status == "KILLED" or .status == "TIMED OUT")] | length' "$report")))
  mapfile -t -O "${#survivors[@]}" survivors < <(jq -r --arg prefix "$dir/$package/" '.files[] | .file_name as $f | .mutations[] | select(.status == "LIVED" or .status == "NOT COVERED") | "\($prefix)\($f):\(.line):\(.column) \(.status) \(.type)"' "$report")
done

echo "go-mutate: $dir: ${#packages[@]} changed packages, killed $killed, survived ${#survivors[@]}"
if [ ${#survivors[@]} -gt 0 ]; then
  printf '  %s\n' "${survivors[@]}"
  echo "go-mutate: add a test that fails on each mutant above, or remove the code no test needs" >&2
  exit 1
fi
