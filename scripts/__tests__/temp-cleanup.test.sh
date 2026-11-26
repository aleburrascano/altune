#!/usr/bin/env bash
set -uo pipefail

HERE=$(cd "$(dirname "$0")/.." && pwd)
FAILURES=0
SCRATCH=$(mktemp -d)
trap 'rm -rf "$SCRATCH"' EXIT

fail() {
    printf 'FAIL: %s\n' "$1"
    FAILURES=$((FAILURES + 1))
}

expect_no_leftovers() {
    local name=$1 dir=$2 leftovers
    leftovers=$(ls -A "$dir")
    [ -z "$leftovers" ] || fail "$name left behind in TMPDIR: $(printf '%s' "$leftovers" | tr '\n' ' ')"
}

run_in_clean_tmp() {
    local name=$1; shift
    local tmp="$SCRATCH/$name"
    mkdir -p "$tmp"
    TMPDIR=$tmp "$@" >"$SCRATCH/$name.log" 2>&1
    CODE=$?
    expect_no_leftovers "$name" "$tmp"
}

run_in_clean_tmp precheck-test env PRECHECK_TEST_MAX_REPOS=2 bash "$HERE/__tests__/precheck.test.sh"
[ "$CODE" = 0 ] || fail "the first two precheck fixtures should pass: $(tail -3 "$SCRATCH/precheck-test.log" | tr '\n' ' ')"

run_in_clean_tmp acq-debug-test bash "$HERE/__tests__/acq-debug.test.sh"

mkdir -p "$SCRATCH/bin"
printf '#!/bin/sh\nexit 22\n' >"$SCRATCH/bin/curl"
chmod +x "$SCRATCH/bin/curl"
run_in_clean_tmp deploy-web-smoke env PATH="$SCRATCH/bin:$PATH" BASE_URL=http://127.0.0.1:1 SHA=0 ENTRY=x bash "$HERE/ci/deploy-web-smoke.sh"
[ "$CODE" != 0 ] || fail "deploy-web-smoke.sh must fail when the site does not answer"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all temp-cleanup checks passed\n'
