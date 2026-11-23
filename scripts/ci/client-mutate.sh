#!/usr/bin/env bash
set -euo pipefail
usage="usage: client-mutate.sh BASE_REF   mutation-tests the client/src lines changed since BASE_REF with Stryker and jest, each file against its own module's tests, and fails on any mutant a test does not catch
  CLIENT_MUTATE_INCREMENTAL_DIR       keeps Stryker's results in this folder, and a later run reuses them while the client files and the merge base are the same
  CLIENT_MUTATE_TIME_LIMIT_SECONDS    stops Stryker after this many seconds in all, keeping the finished mutants in the incremental folder"
[ $# = 1 ] || { echo "$usage" >&2; exit 2; }
base=$1
incremental_dir=${CLIENT_MUTATE_INCREMENTAL_DIR:+$(realpath -m "$CLIENT_MUTATE_INCREMENTAL_DIR")}
time_limit_s=${CLIENT_MUTATE_TIME_LIMIT_SECONDS:-}
if [ -n "$time_limit_s" ]; then
  [[ $time_limit_s =~ ^[1-9][0-9]*$ ]] || { echo "client-mutate: CLIENT_MUTATE_TIME_LIMIT_SECONDS is '$time_limit_s', and must be a whole number of seconds" >&2; exit 2; }
  command -v timeout >/dev/null || { echo "client-mutate: CLIENT_MUTATE_TIME_LIMIT_SECONDS needs timeout(1) from coreutils, which is not on PATH" >&2; exit 3; }
  deadline_s=$((SECONDS + time_limit_s))
fi

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

run_inputs() {
  {
    git merge-base "$base" HEAD
    git ls-files -- . ../scripts/ci/client-mutate.sh | git hash-object --stdin-paths
  } | sha256sum | cut -d' ' -f1
}

incremental_args() {
  [ -n "$incremental_dir" ] || return 0
  printf '%s\n' --incremental --incrementalFile "$incremental_dir/stryker-$1.json" --logLevel info
}

if [ -n "$incremental_dir" ]; then
  mkdir -p "$incremental_dir"
  fingerprint=$(run_inputs)
  if [ "$(cat "$incremental_dir/inputs.sha256" 2>/dev/null)" != "$fingerprint" ]; then
    rm -f "$incremental_dir/stryker-native.json" "$incremental_dir/stryker-web.json"
    echo "$fingerprint" >"$incremental_dir/inputs.sha256"
  fi
fi

time_limited() {
  [ -n "$time_limit_s" ] || { "$@"; return; }
  local left_s=$((deadline_s - SECONDS))
  [ "$left_s" -gt 0 ] || return 124
  timeout --signal=INT --kill-after=2m "$left_s" "$@"
}

forget_crashed_runs() {
  local kept=$incremental_dir/stryker-$1.json
  [ -f "$kept" ] || return 0
  jq '.files[].mutants |= map(select(.status != "RuntimeError"))' "$kept" >"$kept.new" && mv "$kept.new" "$kept"
}

changed_files=$(cut -d: -f1 "$work/ranges" | sort -u | grep -c . || true)
killed=0
reused=0
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
  mapfile -t extra_args < <(incremental_args "$project")
  rc=0
  out=$(CLIENT_MUTATE_JEST_PROJECT="$project" CLIENT_MUTATE_TEST_ROOTS="$test_roots" time_limited "$stryker" run "$work/$project.config.json" --mutate "$mutate" "${extra_args[@]}" 2>&1) || rc=$?
  if [ "$rc" != 0 ]; then
    echo "$out" >&2
    if [ "$rc" = 124 ] || [ "$rc" = 137 ]; then
      kept=
      if [ -n "$incremental_dir" ]; then
        forget_crashed_runs "$project"
        kept=", and the mutants it finished are kept in $incremental_dir: run it again without pushing, and it continues from them"
      fi
      echo "client-mutate: Stryker passed its ${time_limit_s}s time limit on the $project jest project$kept. Reproduce: bash scripts/ci/client-mutate.sh $base" >&2
    elif grep -q 'No tests were executed' <<<"$out"; then
      echo "client-mutate: no test in ${test_roots//:/, } runs ${mutate//,/, } under the $project jest project. Add a test through the module's door (client/src/README.md). Reproduce: bash scripts/ci/client-mutate.sh $base" >&2
    else
      echo "client-mutate: Stryker failed on the $project jest project (a failing test or a broken build stops it). Reproduce: bash scripts/ci/client-mutate.sh $base" >&2
    fi
    exit 1
  fi
  if [ ! -f "$report" ]; then
    grep -q 'No files found for mutation' <<<"$out" && continue
    echo "$out" >&2
    echo "client-mutate: Stryker wrote no report for the $project jest project and did not say why" >&2
    exit 1
  fi
  reused=$((reused + $(sed -nE 's/\x1b\[[0-9;]*m//g; s/.*[^0-9]([0-9]+) of [0-9]+ mutant result\(s\) are reused.*/\1/p' <<<"$out" | grep . || echo 0)))
  killed=$((killed + $(jq '[.files[].mutants[] | select(.status == "Killed" or .status == "Timeout")] | length' "$report")))
  mapfile -t -O "${#survivors[@]}" survivors < <(jq -r '.files | to_entries[] | .key as $file | .value.mutants[] | select(.status == "Survived" or .status == "NoCoverage") | "client/\($file):\(.location.start.line):\(.location.start.column) \(.status) \(.mutatorName) -> \(.replacement // "" | gsub("\\s+"; " ") | .[0:60])"' "$report")
done

echo "client-mutate: $changed_files changed files, killed $killed, survived ${#survivors[@]}, reused $reused"
if [ ${#survivors[@]} -gt 0 ]; then
  printf '  %s\n' "${survivors[@]}"
  echo "client-mutate: add a test that fails on each mutant above, through the module's door (client/src/README.md), or remove the code no test needs. Reproduce: bash scripts/ci/client-mutate.sh $base" >&2
  exit 1
fi
