#!/usr/bin/env bash
set -euo pipefail
usage="usage: client-mutate.sh BASE_REF   mutation-tests the client/src lines changed since BASE_REF with Stryker and jest, each file against its own module's tests, and fails on any mutant a test does not catch"
[ $# = 1 ] || { echo "$usage" >&2; exit 2; }
base=$1

cd "$(git rev-parse --show-toplevel)/client"
stryker=node_modules/.bin/stryker
[ -x "$stryker" ] || { echo "client-mutate: $PWD/$stryker is missing. Install the client's dependencies: bash scripts/worktree-deps.sh client" >&2; exit 3; }
work=$(mktemp -d)
sandbox_prefix=.stryker-tmp-$$
trap 'rm -rf "$work" "$sandbox_prefix"-*' EXIT

git -c core.quotePath=false diff --merge-base "$base" --find-renames --diff-filter=AMR --unified=0 --relative -- src | awk '
  !in_hunks && /^\+\+\+ "/ { print "client-mutate: git quoted the path in \"" $0 "\", which this diff filter cannot read" > "/dev/stderr"; failed = 1; exit 1 }
  /^diff --git / { in_hunks = 0; file = ""; next }
  !in_hunks && /^\+\+\+ b\// { file = substr($0, 7); next }
  /^@@ / {
    in_hunks = 1
    split($3, added, ",")
    start = substr(added[1], 2)
    count = (2 in added) ? added[2] : 1
    if (file ~ /\.tsx?$/ && file !~ /\.d\.ts$/ && file !~ /(^|\/)__tests__\// && file !~ /\.test\.tsx?$/ && count > 0) print file ":" start "-" (start + count - 1)
  }
  END { if (failed) exit 1 }
' >"$work/ranges"

test_root() {
  case $1 in
    src/features/*/* | src/shared/*/*) cut -d/ -f1-3 <<<"$1" ;;
    src/mobile/*) echo src/mobile ;;
    src/app/*) printf 'src/app\n__tests__\n' ;;
    *) dirname "$1" ;;
  esac
}

jest_project() {
  case $1 in
    *.web.ts | *.web.tsx) echo web ;;
    *) echo native ;;
  esac
}

stryker_config() {
  jq --arg report "$work/$1.json" --arg sandbox "$sandbox_prefix-$1" '. + { jsonReporter: { fileName: $report }, tempDirName: $sandbox }' stryker.config.json >"$work/$1.config.json"
}

changed_files=$(cut -d: -f1 "$work/ranges" | sort -u | grep -c . || true)
killed=0
survivors=()
for project in native web; do
  ranges=()
  roots=()
  while IFS= read -r range; do
    file=${range%:*}
    [ "$(jest_project "$file")" = "$project" ] || continue
    ranges+=("$range")
    mapfile -t -O "${#roots[@]}" roots < <(test_root "$file")
  done <"$work/ranges"
  [ ${#ranges[@]} -gt 0 ] || continue
  report=$work/$project.json
  stryker_config "$project"
  test_roots=$(printf '%s\n' "${roots[@]}" | sort -u | paste -sd:)
  mutate=$(IFS=,; echo "${ranges[*]}")
  if ! out=$(env CLIENT_MUTATE_JEST_PROJECT="$project" CLIENT_MUTATE_TEST_ROOTS="$test_roots" "$stryker" run "$work/$project.config.json" --mutate "$mutate" 2>&1); then
    echo "$out" >&2
    echo "client-mutate: Stryker failed on the $project jest project (a failing test or a broken build stops it). Reproduce: bash scripts/ci/client-mutate.sh $base" >&2
    exit 1
  fi
  if [ ! -f "$report" ]; then
    grep -q 'No files marked to be mutated' <<<"$out" && continue
    echo "$out" >&2
    echo "client-mutate: Stryker wrote no report for the $project jest project and did not say why" >&2
    exit 1
  fi
  killed=$((killed + $(jq '[.files[].mutants[] | select(.status == "Killed" or .status == "Timeout")] | length' "$report")))
  mapfile -t -O "${#survivors[@]}" survivors < <(jq -r '.files | to_entries[] | .key as $file | .value.mutants[] | select(.status == "Survived" or .status == "NoCoverage") | "client/\($file):\(.location.start.line):\(.location.start.column) \(.status) \(.mutatorName) -> \(.replacement // "" | gsub("\\s+"; " ") | .[0:60])"' "$report")
done

echo "client-mutate: $changed_files changed files, killed $killed, survived ${#survivors[@]}"
if [ ${#survivors[@]} -gt 0 ]; then
  printf '  %s\n' "${survivors[@]}"
  echo "client-mutate: add a test that fails on each mutant above, through the module's door (client/src/README.md), or remove the code no test needs" >&2
  exit 1
fi
