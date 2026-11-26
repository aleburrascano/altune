#!/usr/bin/env bash
set -uo pipefail

HERE=$(cd "$(dirname "$0")/../.." && pwd)
FAILURES=0
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

mkdir -p "$WORK/repo/.husky" "$WORK/repo/scripts"
cp "$HERE/.husky/pre-push" "$WORK/repo/.husky/pre-push"
cat >"$WORK/repo/scripts/precheck.sh" <<STUB
#!/usr/bin/env bash
printf 'precheck ran with CLIENT_MUTATE_RUNS_ELSEWHERE=%s\n' "\${CLIENT_MUTATE_RUNS_ELSEWHERE:-}" >>"$WORK/calls.log"
exit "\${STUB_RC:-0}"
STUB

ZERO=0000000000000000000000000000000000000000
SHA=1111111111111111111111111111111111111111

run_hook() {
    : >"$WORK/calls.log"
    (cd "$WORK/repo" && printf '%s\n' "$1" | sh -e .husky/pre-push origin url >"$WORK/out.log" 2>&1)
    RC=$?
}

fail() {
    printf 'FAIL: %s\n' "$1"
    FAILURES=$((FAILURES + 1))
}

run_hook "refs/heads/x $SHA refs/heads/x $ZERO"
[ "$RC" = 0 ] || fail "a push of a commit passes when precheck passes (exit $RC)"
grep -qF "CLIENT_MUTATE_RUNS_ELSEWHERE=1" "$WORK/calls.log" || fail "the hook must run precheck with mutation left to CI: $(cat "$WORK/calls.log")"

run_hook "(delete) $ZERO refs/heads/x $SHA"
[ "$RC" = 0 ] || fail "a delete-only push must pass (exit $RC)"
[ ! -s "$WORK/calls.log" ] || fail "a delete-only push must not run precheck"

STUB_RC=3 run_hook "refs/heads/x $SHA refs/heads/x $ZERO"
[ "$RC" = 1 ] || fail "exit 3 from precheck means a tool is missing and blocks the push (exit $RC)"
grep -qF "bash scripts/ci/go-tools.sh" "$WORK/out.log" || fail "exit 3 must say how to install what is missing: $(cat "$WORK/out.log")"
grep -qF "worktree-deps.sh" "$WORK/out.log" || fail "exit 3 must name the dependency installer"
grep -qF "git fetch gitea main" "$WORK/out.log" || fail "exit 3 must also cover a missing base"

STUB_RC=1 run_hook "refs/heads/x $SHA refs/heads/x $ZERO"
[ "$RC" = 1 ] || fail "a failing precheck blocks the push (exit $RC)"
grep -qF "checks failed" "$WORK/out.log" || fail "a blocked push must say checks failed"

FACTORY_SANDBOX=1 run_hook "refs/heads/x $SHA refs/heads/x $ZERO"
[ -s "$WORK/calls.log" ] || fail "no environment variable may turn the hook off: precheck must run with FACTORY_SANDBOX=1"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all pre-push checks passed\n'
