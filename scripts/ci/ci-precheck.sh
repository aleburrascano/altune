#!/usr/bin/env bash
set -euo pipefail
usage="usage: ci-precheck.sh   run in CI from the checkout: precheck at PR scope against origin/main, or on a push to main against the commit before the push (BEFORE, else HEAD~1), with the whole go-api module linted and mutation left to its own job"
[ $# = 0 ] || { echo "$usage" >&2; exit 2; }
root=$(git rev-parse --show-toplevel)
cd "$root"
base=origin/main
if [ "${GITHUB_REF:-}" = refs/heads/main ]; then
  before=${BEFORE:-}
  if [ -n "${before//0/}" ] && git rev-parse --verify --quiet "$before^{commit}" >/dev/null; then
    base=$before
  else
    base=$(git rev-parse --verify --quiet HEAD~1) || { echo "ci-precheck: there is no commit before this push to main, so nothing was checked" >&2; exit 2; }
  fi
fi
export PRECHECK_LINT_WHOLE=1 CLIENT_MUTATE_RUNS_ELSEWHERE=1
exec bash scripts/precheck.sh "$base"
