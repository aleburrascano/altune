#!/usr/bin/env bash

# Self-test for the `changes` docs-only filter in deploy-backend.yml, in the same
# shape as the deploy/*_test.sh scripts: fixture commits in a scratch repo stand in
# for a push, and the filter's own deploy=true/false output is the assertion.
#
# The script under test is LIFTED OUT OF THE YAML rather than retyped, so the thing
# asserted is the thing Actions runs. Two directions matter and only one is loud:
# a false deploy=true costs one needless prod-gate approval, while a false
# deploy=false silently swallows a real prod deploy (#1553 — why the exclusion is
# *.md and never a directory name like */docs/*). GitHub only parses *.yml in this
# directory, so this file sits inert beside the workflow. Run it by hand:
#   bash .github/workflows/deploy-backend_test.sh

set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
FAILURES=0
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

FILTER="$WORK/filter.sh"
awk '
    /^      - id: filter$/ { in_step = 1 }
    in_step && /^        run: \|$/ { in_run = 1; next }
    in_run && /^          / { sub(/^          /, ""); print; next }
    in_run && NF { exit }
' "$HERE/deploy-backend.yml" >"$FILTER"

if ! grep -q 'deploy=true' "$FILTER"; then
    printf 'FAIL: could not lift the filter step out of deploy-backend.yml\n'
    exit 1
fi

git init -q -b main "$WORK/repo"
git -C "$WORK/repo" config user.email test@altune.local
git -C "$WORK/repo" config user.name "filter test"
mkdir -p "$WORK/repo/services/go-api"
echo base >"$WORK/repo/services/go-api/main.go"
git -C "$WORK/repo" add -A
git -C "$WORK/repo" commit -qm base
BASE=$(git -C "$WORK/repo" rev-parse HEAD)

fail() {
    printf 'FAIL: %s\n      %s\n' "$CASE" "$1"
    FAILURES=$((FAILURES + 1))
}

# expect_deploy <true|false> <changed path>...
expect_deploy() {
    local want=$1 f
    shift
    git -C "$WORK/repo" reset -q --hard "$BASE"
    for f in "$@"; do
        mkdir -p "$WORK/repo/$(dirname "$f")"
        echo change >>"$WORK/repo/$f"
    done
    git -C "$WORK/repo" add -A
    git -C "$WORK/repo" commit -qm "$CASE"

    local got
    got=$(run_filter push "$BASE" "$(git -C "$WORK/repo" rev-parse HEAD)")
    [ "$got" = "$want" ] || fail "expected deploy=$want, got deploy=$got"
}

# run_filter <event> <before> <after> -> the deploy= value the step would output.
run_filter() {
    local out="$WORK/output"
    : >"$out"
    (cd "$WORK/repo" && EVENT=$1 BEFORE=$2 AFTER=$3 GITHUB_OUTPUT="$out" \
        bash "$FILTER" >"$WORK/out.log" 2>&1)
    sed -n 's/^deploy=//p' "$out"
}

CASE="a deployable .go file under a docs/ dir still deploys"
expect_deploy true services/go-api/internal/docs/registry.go

CASE="a non-.md asset under a docs/ dir still deploys"
expect_deploy true services/overseer/docs/dashboard.tmpl

CASE="a docs-only push (*.md) skips the deploy chain"
expect_deploy false services/go-api/RUNBOOK.md docs/features/deploy/design.md

CASE="an .md inside a services docs/ dir still skips"
expect_deploy false services/go-api/internal/docs/guide.md

CASE="ordinary backend code deploys"
expect_deploy true services/go-api/internal/app/service.go

CASE="a doc alongside real code deploys"
expect_deploy true services/go-api/RUNBOOK.md services/go-api/main.go

CASE="a push touching nothing in the backend skips"
expect_deploy false apps/mobile/App.tsx

CASE="a manual dispatch has no diff base and fails safe"
got=$(run_filter workflow_dispatch "" HEAD)
[ "$got" = true ] || fail "expected deploy=true, got deploy=$got"

CASE="a first push of a branch (null before) fails safe"
got=$(run_filter push 0000000000000000000000000000000000000000 HEAD)
[ "$got" = true ] || fail "expected deploy=true, got deploy=$got"

CASE="a force-pushed-away diff base fails safe"
got=$(run_filter push 1111111111111111111111111111111111111111 HEAD)
[ "$got" = true ] || fail "expected deploy=true, got deploy=$got"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all deploy path-filter checks passed\n'
