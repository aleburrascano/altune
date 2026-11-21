#!/usr/bin/env bash
set -euo pipefail
usage="usage: go-mutate.sh BASE_REF MODULE_DIR   mutation-tests the lines MODULE_DIR changed since BASE_REF, and fails on any mutant a test does not catch"
[ $# = 2 ] || { echo "$usage" >&2; exit 2; }
base=$1
dir=$2
gremlins=github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0

cd "$dir"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
report=$work/report.json

diff_paths_match_gremlins_paths=(env GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=diff.relative GIT_CONFIG_VALUE_0=true)
if ! out=$("${diff_paths_match_gremlins_paths[@]}" go run "$gremlins" unleash --diff "$base" --output "$report" . 2>&1); then
  echo "$out" >&2
  echo "go-mutate: gremlins failed in $dir (a failing test or build stops it)" >&2
  exit 1
fi
if [ ! -f "$report" ]; then
  grep -q 'No results to report.' <<<"$out" && { echo "go-mutate: no changed line in $dir has anything to mutate"; exit 0; }
  echo "$out" >&2
  echo "go-mutate: gremlins wrote no report and did not say why" >&2
  exit 1
fi

summary=$(jq -r '[.files[].mutations[] | .status] | "killed \(map(select(. == "KILLED" or . == "TIMED OUT")) | length), survived \(map(select(. == "LIVED" or . == "NOT COVERED")) | length)"' "$report")
survivors=$(jq -r '.files[] | .file_name as $f | .mutations[] | select(.status == "LIVED" or .status == "NOT COVERED") | "\($f):\(.line):\(.column) \(.status) \(.type)"' "$report")
echo "go-mutate: $dir: $summary"
if [ -n "$survivors" ]; then
  echo "$survivors" | sed "s#^#  $dir/#"
  echo "go-mutate: add a test that fails on each mutant above, or remove the code no test needs" >&2
  exit 1
fi
