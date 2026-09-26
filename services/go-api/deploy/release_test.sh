#!/usr/bin/env bash

set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
FAILURES=0
SHA=0123456789abcdef0123456789abcdef01234567

setup_case() {
    local failing=${STUB_FAIL:-none}
    WORK=$(mktemp -d)
    mkdir -p "$WORK/bin" "$WORK/altune/services/go-api/deploy"
    for step in staging prod-migrate blue-green overseer smoke; do
        cat >"$WORK/altune/services/go-api/deploy/$step.sh" <<EOF
printf '%s %s container=%s\n' "$step" "\$*" "\${SMOKE_GOAPI_CONTAINER:-}" >> "$WORK/steps.log"
[ "$step" != "$failing" ]
EOF
    done
    printf 'active_color() { printf green; }\n' >"$WORK/altune/services/go-api/deploy/lib.sh"
    for tool in git docker; do
        cat >"$WORK/bin/$tool" <<EOF
#!/usr/bin/env bash
printf '%s %s\n' "$tool" "\$*" >> "$WORK/steps.log"
EOF
    done
    chmod +x "$WORK/bin"/*
    : >"$WORK/steps.log"
    unset STUB_FAIL
}

release() {
    (PATH="$WORK/bin:$PATH" ALTUNE_DIR="$WORK/altune" LOCK_FILE="$WORK/lock" LOCK_TIMEOUT=2 \
        bash "$HERE/release.sh" "$@" >"$WORK/out.log" 2>&1)
    RC=$?
}

fail() {
    printf 'FAIL: %s\n      %s\n' "$CASE" "$1"
    FAILURES=$((FAILURES + 1))
}

expect_rc() {
    [ "$RC" = "$1" ] || fail "expected exit $1, got $RC ($(tail -1 "$WORK/out.log"))"
}

expect_steps() {
    local got
    got=$(cut -d' ' -f1 "$WORK/steps.log" | tr '\n' ' ')
    [ "$got" = "$1" ] || fail "expected steps '$1', got '$got'"
}

expect_step() {
    grep -qxF "$1" "$WORK/steps.log" || fail "expected step '$1' in: $(tr '\n' '|' <"$WORK/steps.log")"
}

CASE="staging resets the checkout to the sha and deploys only staging"
setup_case
release staging "$SHA"
expect_rc 0
expect_steps "git git git staging docker "
expect_step "git -C $WORK/altune reset --hard $SHA"
expect_step "docker image prune -f"

CASE="prod migrates, flips, deploys the overseer, then smokes the live colour at the sha"
setup_case
release prod "$SHA" https://prod.example.test
expect_rc 0
expect_steps "git git git prod-migrate blue-green overseer smoke docker "
expect_step "smoke https://prod.example.test altune-overseer $SHA container=altune-go-api-green"

CASE="a failed migration stops prod before the flip"
STUB_FAIL=prod-migrate setup_case
release prod "$SHA" https://prod.example.test
expect_rc 1
expect_steps "git git git prod-migrate "

CASE="a failed prod smoke fails the release"
STUB_FAIL=smoke setup_case
release prod "$SHA" https://prod.example.test
expect_rc 1

CASE="prod without a public url fails before touching the checkout"
setup_case
release prod "$SHA"
expect_rc 1
expect_steps ""

CASE="an unknown tier fails before touching the checkout"
setup_case
release canary "$SHA"
expect_rc 1
expect_steps ""

CASE="a short or moving ref fails before touching the checkout"
for bad in main 0123456 "$SHA "; do
    setup_case
    release staging "$bad"
    expect_rc 1
    expect_steps ""
done

CASE="a held lock times the release out before touching the checkout"
setup_case
exec 8>"$WORK/lock"
flock 8
(PATH="$WORK/bin:$PATH" ALTUNE_DIR="$WORK/altune" LOCK_FILE="$WORK/lock" LOCK_TIMEOUT=1 \
    bash "$HERE/release.sh" staging "$SHA" 8>&- >"$WORK/out.log" 2>&1)
RC=$?
exec 8>&-
expect_rc 1
expect_steps ""

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all release checks passed\n'
