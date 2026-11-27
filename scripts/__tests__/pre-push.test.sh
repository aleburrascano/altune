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

G=$WORK/git
export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@t GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@t
mkdir -p "$G/.husky" "$G/scripts/ci"
cp "$HERE/.husky/pre-push" "$G/.husky/pre-push"
cp "$HERE/scripts/ci/merge-result.sh" "$G/scripts/ci/merge-result.sh"
cat >"$G/scripts/precheck.sh" <<STUB
#!/usr/bin/env bash
printf 'precheck in %s args %s main-file %s\n' "\$PWD" "\$*" "\$([ -f from-main.txt ] && echo yes || echo no)" >>"$WORK/calls.log"
exit 0
STUB
git -C "$G" init -q -b main
printf 'a\n' >"$G/shared.txt"
git -C "$G" add -A && git -C "$G" commit -qm base
git -C "$G" remote add gitea "$WORK/none.git"
git -C "$G" update-ref refs/remotes/gitea/main HEAD
git -C "$G" checkout -q -b feature
printf 'b\n' >"$G/feature.txt"
git -C "$G" add -A && git -C "$G" commit -qm feature
run_git_hook() {
    : >"$WORK/calls.log"
    (cd "$G" && printf '%s\n' "refs/heads/feature $(git rev-parse HEAD) refs/heads/feature $ZERO" | XDG_CACHE_HOME="$WORK/cache" sh -e .husky/pre-push gitea url >"$WORK/out.log" 2>&1)
    RC=$?
}

run_git_hook
[ "$RC" = 0 ] || fail "a branch that already holds main passes (exit $RC)"
grep -qF "precheck in $G args --full gitea/main main-file no" "$WORK/calls.log" || fail "a branch that holds main is checked in place, against main: $(cat "$WORK/calls.log")"

git -C "$G" checkout -q main
printf 'new\n' >"$G/from-main.txt"
git -C "$G" add -A && git -C "$G" commit -qm "main moves"
git -C "$G" update-ref refs/remotes/gitea/main HEAD
git -C "$G" checkout -q feature
run_git_hook
[ "$RC" = 0 ] || fail "a branch behind main that merges cleanly passes (exit $RC): $(cat "$WORK/out.log")"
grep -qE "precheck in $WORK/cache/altune/prepush-merge/[0-9]+ args --full gitea/main main-file yes" "$WORK/calls.log" || fail "a branch behind main is checked as merged with main, the tree CI checks, in a cache directory: $(cat "$WORK/calls.log")"
case $(cat "$WORK/calls.log") in */.git/*) fail "the merge check must not run under a .git folder, which jest ignores: $(cat "$WORK/calls.log")" ;; esac
grep -qF "the merge result" "$WORK/out.log" || fail "the hook must say it checks the merge result: $(cat "$WORK/out.log")"
[ "$(git -C "$G" rev-parse HEAD)" = "$(git -C "$G" rev-parse feature)" ] || fail "checking the merge result must not move the branch"
[ -z "$(git -C "$G" status --porcelain)" ] || fail "checking the merge result must leave the checkout clean"

git -C "$G" checkout -q main
printf 'main edit\n' >"$G/shared.txt"
git -C "$G" add -A && git -C "$G" commit -qm "main edits shared"
git -C "$G" update-ref refs/remotes/gitea/main HEAD
git -C "$G" checkout -q feature
git -C "$G" reset -q --hard HEAD~0
printf 'feature edit\n' >"$G/shared.txt"
git -C "$G" add -A && git -C "$G" commit -qm "feature edits shared"
run_git_hook
[ "$RC" = 1 ] || fail "a branch that conflicts with main blocks the push (exit $RC)"
grep -qF "conflicts with gitea/main" "$WORK/out.log" || fail "a conflicting branch must be told to rebase: $(cat "$WORK/out.log")"
[ ! -s "$WORK/calls.log" ] || fail "a conflicting branch must not run precheck"

git -C "$G" reset -q --hard HEAD~1
git -C "$G" checkout -q main
git -C "$G" update-ref refs/remotes/gitea/main HEAD
git -C "$G" checkout -q feature
git -C "$G" reset -q --hard HEAD~0

cache_dir=$(ls -d "$WORK"/cache/altune/prepush-merge/[0-9]* 2>/dev/null | head -n 1)
rm -f "$cache_dir/.git"
run_git_hook
[ "$RC" = 0 ] || fail "a half-made merge check directory is made again (exit $RC): $(cat "$WORK/out.log")"
grep -qE "precheck in $WORK/cache/altune/prepush-merge/[0-9]+ args" "$WORK/calls.log" || fail "after recovery the merge check must run in its directory: $(cat "$WORK/calls.log")"

(
    exec 8>"$cache_dir.lock"
    flock 8
    : >"$WORK/calls.log"
    (exec 8>&-; cd "$G" && printf '%s\n' "refs/heads/feature $(git rev-parse HEAD) refs/heads/feature $ZERO" | XDG_CACHE_HOME="$WORK/cache" sh -e .husky/pre-push gitea url >"$WORK/out2.log" 2>&1) &
    hook=$!
    sleep 2
    if [ -s "$WORK/calls.log" ]; then echo "ran while the lock was held" >"$WORK/lock.result"; else echo "waited" >"$WORK/lock.result"; fi
    exec 8>&-
    wait "$hook"
)
grep -qx waited "$WORK/lock.result" || fail "a second push from the same checkout must wait for the first one's merge check"
grep -qE "precheck in $WORK/cache" "$WORK/calls.log" || fail "the waiting push must run once the lock is free: $(cat "$WORK/calls.log")"

printf 'gitdir: %s/nowhere\n' "$WORK" >"$cache_dir/.git"
run_git_hook
[ "$RC" = 0 ] || fail "a merge check directory whose .git points at a dead registration heals itself (exit $RC): $(cat "$WORK/out.log")"
grep -qE "precheck in $WORK/cache/altune/prepush-merge/[0-9]+ args" "$WORK/calls.log" || fail "after healing the merge check must run in its directory: $(cat "$WORK/calls.log")"
grep -q "^gitdir: $G/.git/worktrees/" "$cache_dir/.git" || fail "the healed directory must be a live worktree of the repo: $(cat "$cache_dir/.git")"

stale=$WORK/cache/altune/prepush-merge/999999999
mkdir -p "$stale"
printf 'x\n' >"$stale/file"
printf '%s\n' "$WORK/no-such-checkout" >"$stale.src"
: >"$stale.lock"
run_git_hook
[ "$RC" = 0 ] || fail "a push with a stale scratch directory still passes (exit $RC)"
[ ! -e "$stale" ] && [ ! -e "$stale.src" ] && [ ! -e "$stale.lock" ] || fail "a scratch worktree whose source checkout is gone is removed on the next push"
[ -e "$cache_dir/.git" ] || fail "the scratch worktree of a checkout that still exists is kept"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all pre-push checks passed\n'
