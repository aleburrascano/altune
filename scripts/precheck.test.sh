#!/usr/bin/env bash
set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
FAILURES=0

new_repo() {
    WORK=$(mktemp -d)
    mkdir -p "$WORK/repo/scripts"
    cp "$HERE/precheck.sh" "$WORK/repo/scripts/"
    (
        cd "$WORK/repo" || exit 1
        git init -qb main
        git config user.email test@example.com
        git config user.name test
        printf 'package keep\n' >keep.go
        git add -A
        git commit -qm baseline
    )
}

add_commented_shell_file() {
    (
        cd "$WORK/repo" || exit 1
        printf '%s\n%s\n' '#!/usr/bin/env bash' '# comment' >added.sh
        git add added.sh
        git commit -qm "add commented shell file"
    )
}

run_script() {
    (
        cd "$WORK/repo" || exit 1
        bash scripts/precheck.sh "$@" >"$WORK/out.log" 2>&1
    )
    RC=$?
}

fail() {
    printf 'FAIL: %s\n      %s\n' "$CASE" "$1"
    FAILURES=$((FAILURES + 1))
}

expect_rc() {
    [ "$RC" = "$1" ] || fail "expected exit $1, got $RC ($(tail -1 "$WORK/out.log"))"
}

expect_out() {
    grep -qF "$1" "$WORK/out.log" || fail "expected output to mention '$1'"
}

CASE="default uses gitea/main when both gitea/main and origin/main exist, so a fresh base commit is green"
new_repo
(
    cd "$WORK/repo" || exit 1
    git update-ref refs/remotes/origin/main HEAD
)
add_commented_shell_file
(
    cd "$WORK/repo" || exit 1
    git update-ref refs/remotes/gitea/main HEAD
)
run_script
expect_rc 0
expect_out "precheck: green"

CASE="default falls back to origin/main when gitea/main is absent, so a fresh base commit is green"
new_repo
add_commented_shell_file
(
    cd "$WORK/repo" || exit 1
    git update-ref refs/remotes/origin/main HEAD
)
run_script
expect_rc 0
expect_out "precheck: green"

CASE="an explicit ref argument still overrides the default and finds the added comment"
new_repo
(
    cd "$WORK/repo" || exit 1
    git branch explicit-base HEAD
)
add_commented_shell_file
(
    cd "$WORK/repo" || exit 1
    git update-ref refs/remotes/origin/main HEAD
    git update-ref refs/remotes/gitea/main HEAD
)
run_script explicit-base
expect_rc 1
expect_out "precheck: red"
expect_out "rerun: bash scripts/precheck.sh explicit-base"

CASE="an explicit ref with no merge base names that ref in the message and exits 3"
new_repo
(
    cd "$WORK/repo" || exit 1
    git checkout -q --orphan unrelated
    git commit -q --allow-empty -m unrelated
    git branch -f unreachable-base unrelated
    git checkout -q main
)
run_script unreachable-base
expect_rc 3
expect_out "precheck: no merge base with unreachable-base"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all precheck checks passed\n'
