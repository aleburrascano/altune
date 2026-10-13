#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/ci-budget.mjs
fail=0

t() {
  local desc=$1 durations=$2 want=$3
  got=$(node --input-type=module -e "
import { verdicts } from '$script';
const [v] = verdicts({ 'gitea/x.yml': 100 }, { 'gitea/x.yml': $durations });
console.log(JSON.stringify({ median: v.median, over: v.over, skipped: v.skipped }));
" 2>&1)
  [ "$got" = "$want" ] || { echo "FAIL $desc: want $want got $got"; fail=1; }
}

t "median under budget" "[50, 60, 70]" '{"median":60,"over":false,"skipped":false}'
t "median over budget" "[150, 160, 20]" '{"median":150,"over":true,"skipped":false}'
t "even count averages the middle two" "[10, 90, 100, 200]" '{"median":95,"over":false,"skipped":false}'
t "fewer than 3 runs is skipped" "[500, 600]" '{"median":null,"over":false,"skipped":true}'
t "no runs is skipped" "[]" '{"median":null,"over":false,"skipped":true}'

[ "$fail" = 0 ] && echo "ci-budget tests ok"
exit $fail
