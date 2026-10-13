#!/usr/bin/env bash
cd "$(dirname "$0")/../.." || exit 1
pd=$PWD/ci/pr-ci-dupes.mjs
fail=0
repo=$(mktemp -d); trap 'rm -rf "$repo"' EXIT
mkdir -p "$repo/.gitea/workflows" "$repo/.github/workflows" "$repo/scripts/ci"

cat >"$repo/scripts/ci/integration.sh" <<'SH'
set -e
INTEGRATION=1 go test -count=1 ./...
SH
cat >"$repo/.github/workflows/pr-checks.yml" <<'YML'
on:
  pull_request:
jobs:
  backend-tests:
    steps:
      - name: tests
        run: go test -shuffle=on ./...
YML

t() {
  local want=$1 desc=$2 grep_for=${3:-}
  out=$(node "$pd" "$repo" 2>&1); got=$?
  if [ "$got" != "$want" ] || { [ -n "$grep_for" ] && ! grep -qF -- "$grep_for" <<<"$out"; }; then
    echo "FAIL want $want got $got: $desc"; echo "$out" | sed 's/^/    /'; fail=1
  fi
}

cat >"$repo/.gitea/workflows/precheck.yml" <<'YML'
on:
  pull_request:
jobs:
  precheck:
    steps:
      - name: integration
        run: bash scripts/ci/integration.sh "gitea/main"
YML
t 1 "go test in a PR script and in pr-checks is named" "pr-ci-dupes: go test ./... runs in precheck.yml#precheck via scripts/ci/integration.sh and pr-checks.yml#backend-tests"

cat >"$repo/.gitea/workflows/precheck.yml" <<'YML'
on:
  pull_request:
jobs:
  precheck:
    steps:
      - name: lint
        run: echo lint
YML
t 0 "one place running go test is fine"

cat >"$repo/.gitea/workflows/precheck.yml" <<'YML'
on:
  pull_request:
jobs:
  precheck:
    steps:
      - name: lint
        run: echo lint
YML
cat >"$repo/.gitea/workflows/main-only.yml" <<'YML'
on:
  push:
    branches: [main]
jobs:
  unit:
    steps:
      - name: jest on push
        run: npx jest
YML
cat >"$repo/.github/workflows/pr-checks.yml" <<'YML'
on:
  pull_request:
jobs:
  js:
    steps:
      - name: jest
        run: npx jest
YML
t 0 "a push-only workflow is not a PR place"

cat >"$repo/.gitea/workflows/precheck.yml" <<'YML'
on:
  pull_request:
jobs:
  precheck:
    steps:
      - name: jest
        run: npx jest
YML
t 1 "jest in a PR workflow and pr-checks is named" "pr-ci-dupes: jest runs in precheck.yml#precheck and pr-checks.yml#js"

[ $fail = 0 ] && echo "pr-ci-dupes.test.sh: all cases pass"
exit $fail
