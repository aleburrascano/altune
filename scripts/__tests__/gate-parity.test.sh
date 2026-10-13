#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
gp=$PWD/gate-parity.mjs
fail=0
repo=$(mktemp -d); trap 'rm -rf "$repo"' EXIT
mkdir -p "$repo/.gitea/workflows" "$repo/scripts/ci"

cat >"$repo/.gitea/workflows/ci.yml" <<'YML'
on:
  push:
    branches: [main]
jobs:
  build:
    steps:
      - name: checkout
        run: true
      - name: unit tests
        run: true
YML
cat >"$repo/.gitea/workflows/nightly.yml" <<'YML'
on:
  schedule:
    - cron: "0 0 * * *"
jobs:
  sweep:
    steps:
      - name: not on main
        run: true
YML
cat >"$repo/.gitea/workflows/precheck.yml" <<'YML'
on:
  pull_request:
jobs:
  precheck:
    steps:
      - name: precheck
        run: bash scripts/ci/integration.sh "gitea/main"
YML
cat >"$repo/scripts/precheck.sh" <<'SH'
check "unit tests" . true
test_check "slow tests" . true
jest_check "jest tests" . true
SH

manifest() { printf '%s\n' "$1" >"$repo/scripts/ci/gate-parity.json"; }

t() {
  local want=$1 desc=$2 grep_for=${3:-}
  out=$(node "$gp" "$repo" 2>&1); got=$?
  if [ "$got" != "$want" ] || { [ -n "$grep_for" ] && ! grep -qF -- "$grep_for" <<<"$out"; }; then
    echo "FAIL want $want got $got: $desc"; echo "$out" | sed 's/^/    /'; fail=1
  fi
}

manifest '{"ci.yml#build#checkout":{"allow":"plumbing"},"ci.yml#build#unit tests":{"gate":"unit tests"}}'
t 0 "every main step has a gate or an allow reason"

manifest '{"ci.yml#build#checkout":{"allow":"plumbing"}}'
t 1 "a main step missing from the manifest is named" "gate-parity: ci.yml#build#unit tests has no gate counterpart or allow reason"

manifest '{"ci.yml#build#checkout":{"allow":"  "},"ci.yml#build#unit tests":{"gate":"unit tests"}}'
t 1 "an allow with an empty reason is named" "gate-parity: ci.yml#build#checkout has an allow with an empty reason"

manifest '{"ci.yml#build#checkout":{"allow":"plumbing"},"ci.yml#build#unit tests":{"gate":"lint"}}'
t 1 "a gate precheck does not define is named" 'gate-parity: ci.yml#build#unit tests names gate "lint", which precheck does not define'

manifest '{"ci.yml#build#checkout":{"allow":"plumbing"},"ci.yml#build#unit tests":{"gate":"unit tests"},"ci.yml#build#gone":{"allow":"old"}}'
t 1 "a stale key is named" "gate-parity: ci.yml#build#gone is stale: no main-triggered step matches it"

manifest '{"ci.yml#build#checkout":{"allow":"plumbing"},"ci.yml#build#unit tests":{"gate":"slow tests"}}'
t 0 "a test_check name counts as a gate"

manifest '{"ci.yml#build#checkout":{"allow":"plumbing"},"ci.yml#build#unit tests":{"gate":"jest tests"}}'
t 0 "a jest_check name counts as a gate"

manifest '{"ci.yml#build#checkout":{"allow":"plumbing"},"ci.yml#build#unit tests":{"gate":"integration.sh"}}'
t 0 "a script the gate workflow runs counts as a gate"

mkdir -p "$repo/.github/workflows"
cat >"$repo/.github/workflows/pr-checks.yml" <<'YML'
on:
  push:
    branches: ["pr/**"]
jobs:
  backend-tests:
    steps:
      - name: tests
        run: true
YML

manifest '{"ci.yml#build#checkout":{"allow":"plumbing"},"ci.yml#build#unit tests":{"gate":"pr-checks/backend-tests"}}'
t 0 "a pr-checks job counts as a gate"

manifest '{"ci.yml#build#checkout":{"allow":"plumbing"},"ci.yml#build#unit tests":{"gate":"pr-checks/nope"}}'
t 1 "a pr-checks job that does not exist is named" 'names gate "pr-checks/nope", which precheck does not define'

[ $fail = 0 ] && echo "gate-parity.test.sh: all cases pass"
exit $fail
