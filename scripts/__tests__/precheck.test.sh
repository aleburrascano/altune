#!/usr/bin/env bash
set -uo pipefail

HERE=$(cd "$(dirname "$0")/.." && pwd)
FAILURES=0

new_repo() {
    WORK=$(mktemp -d)
    mkdir -p "$WORK/repo/scripts/__tests__"
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

add_plain_file() {
    (
        cd "$WORK/repo" || exit 1
        printf 'plain\n' >added.txt
        git add added.txt
        git commit -qm "add plain file"
    )
}

add_failing_nocomments_check() {
    (
        cd "$WORK/repo" || exit 1
        mkdir -p services/go-api/scripts/nocomments
        printf 'module fixture/go-api\n\ngo 1.26.6\n' >services/go-api/go.mod
        printf 'package main\n\nimport "os"\n\nfunc main() {\n\tos.Exit(1)\n}\n' >services/go-api/scripts/nocomments/main.go
        git add -A
        git commit -qm "add failing nocomments check"
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
add_plain_file
(
    cd "$WORK/repo" || exit 1
    git update-ref refs/remotes/gitea/main HEAD
)
run_script
expect_rc 0
expect_out "precheck: green"

CASE="default falls back to origin/main when gitea/main is absent, so a fresh base commit is green"
new_repo
add_plain_file
(
    cd "$WORK/repo" || exit 1
    git update-ref refs/remotes/origin/main HEAD
)
run_script
expect_rc 0
expect_out "precheck: green"

CASE="an explicit ref argument still overrides the default and finds a failing check"
new_repo
(
    cd "$WORK/repo" || exit 1
    git branch explicit-base HEAD
)
add_failing_nocomments_check
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

CASE="a failing nocomments check fails precheck, and it runs though the PR touches no shell"
new_repo
(
    cd "$WORK/repo" || exit 1
    mkdir -p services/go-api/scripts/nocomments
    printf 'module fixture/go-api\n\ngo 1.26.6\n' >services/go-api/go.mod
    printf 'package main\n\nimport "os"\n\nfunc main() {\n\tprintln("added.txt:1")\n\tos.Exit(1)\n}\n' >services/go-api/scripts/nocomments/main.go
    git add -A
    git commit -qm "add nocomments fixture"
    git update-ref refs/remotes/gitea/main HEAD
)
run_script
expect_rc 1
expect_out "FAIL  no comments (whole tree)"
expect_out "added.txt:1"

CASE="a clean nocomments check keeps precheck green"
new_repo
(
    cd "$WORK/repo" || exit 1
    mkdir -p services/go-api/scripts/nocomments
    printf 'module fixture/go-api\n\ngo 1.26.6\n' >services/go-api/go.mod
    printf 'package main\n\nfunc main() {}\n' >services/go-api/scripts/nocomments/main.go
    git add -A
    git commit -qm "add nocomments fixture"
    git update-ref refs/remotes/gitea/main HEAD
)
run_script
expect_rc 0
expect_out "ok    no comments (whole tree)"

add_mobile_stubs() {
    mkdir -p "$WORK/bin" "$WORK/home"
    printf '#!/usr/bin/env bash\nprintf "npx %%s\\n" "$*" >>"%s/npx.log"\n' "$WORK" >"$WORK/bin/npx"
    chmod +x "$WORK/bin/npx"
    (
        cd "$WORK/repo" || exit 1
        mkdir -p client
        printf 'x\n' >client/.keep
        printf 'echo true\n' >scripts/worktree-deps.sh
        git add -A
        git commit -qm "mobile fixture"
        git update-ref refs/remotes/gitea/main HEAD
    )
}

run_script_with_stubs() {
    (
        cd "$WORK/repo" || exit 1
        HOME="$WORK/home" PATH="$WORK/bin:$PATH" bash scripts/precheck.sh "$@" >"$WORK/out.log" 2>&1
    )
    RC=$?
}

expect_npx() {
    grep -qF -- "$1" "$WORK/npx.log" 2>/dev/null || fail "expected npx call to include '$1'"
}

CASE="mobile related tests keep the jest cache under HOME, not the /tmp tmpfs"
new_repo
add_mobile_stubs
(
    cd "$WORK/repo" || exit 1
    mkdir -p client/src
    printf 'export const x = 1;\n' >client/src/x.ts
    git add -A
    git commit -qm "mobile change"
)
run_script_with_stubs
expect_npx "--findRelatedTests"
expect_npx "--cacheDirectory $WORK/home/.cache/altune-ci/jest"

CASE="mobile event contracts keep the jest cache under HOME, not the /tmp tmpfs"
new_repo
add_mobile_stubs
(
    cd "$WORK/repo" || exit 1
    mkdir -p services/go-api/internal/shared/events
    printf 'package events\n' >services/go-api/internal/shared/events/e.go
    git add -A
    git commit -qm "events change"
)
run_script_with_stubs
expect_npx "eventContract.test.ts"
expect_npx "--cacheDirectory $WORK/home/.cache/altune-ci/jest"

add_gate_parity_stub() {
    (
        cd "$WORK/repo" || exit 1
        printf 'process.exit(%s);\n' "$1" >scripts/gate-parity.mjs
        printf 'echo true\n' >scripts/worktree-deps.sh
        git add -A
        git commit -qm "gate parity fixture"
        git update-ref refs/remotes/gitea/main HEAD
        printf 'notes\n' >README.md
        git add -A
        git commit -qm "docs only"
    )
}

CASE="gate parity runs on a change that touches no code"
new_repo
add_gate_parity_stub 0
run_script
expect_rc 0
expect_out "ok    gate parity with main"

CASE="a gate parity gap turns precheck red"
new_repo
add_gate_parity_stub 1
run_script
expect_rc 1
expect_out "FAIL  gate parity with main"

add_jest_stub() {
    cat >"$WORK/bin/npx" <<'STUB'
#!/usr/bin/env bash
printf 'npx %s\n' "$*" >>"$NPX_LOG"
case " $* " in *" jest "*) ;; *) exit 0 ;; esac
case "${STUB_JEST:-pass}" in
    fail)
        echo "FAIL src/__tests__/a.test.ts"
        echo "  ● library rows › shows the artist name"
        echo ""
        echo "    expect(received).toBe(expected)"
        echo ""
        echo "    Expected: \"Ada Lovelace\""
        echo "    Received: \"\""
        for i in $(seq 1 80); do echo "    at frame $i (node_modules/x.js:$i:1)"; done
        echo "  ● Console"
        echo "  ● player › resumes after a seek"
        for i in $(seq 1 80); do echo "    at frame $i (node_modules/y.js:$i:1)"; done
        echo "Summary of all failing tests"
        echo "  ● library rows › shows the artist name"
        echo "Test Suites: 1 failed, 3 passed, 4 total"
        echo "Tests:       2 failed, 40 passed, 42 total"
        exit 1
        ;;
    hang) exec sleep 60 ;;
esac
exit 0
STUB
    chmod +x "$WORK/bin/npx"
}

mobile_src_change() {
    (
        cd "$WORK/repo" || exit 1
        mkdir -p client/src
        printf 'export const x = 1;\n' >client/src/x.ts
        git add -A
        git commit -qm "mobile change"
    )
}

CASE="mobile related tests run with --forceExit and --watchman=false"
new_repo
add_mobile_stubs
add_jest_stub
mobile_src_change
NPX_LOG="$WORK/npx.log" run_script_with_stubs
expect_npx "--forceExit"
expect_npx "--watchman=false"
expect_out "ok    mobile tests (related)"

CASE="a failing jest run prints each failed test name, not the last 40 lines"
new_repo
add_mobile_stubs
add_jest_stub
mobile_src_change
STUB_JEST=fail NPX_LOG="$WORK/npx.log" run_script_with_stubs
expect_rc 1
expect_out "FAIL  mobile tests (related)"
expect_out "failed tests:"
expect_out "● library rows › shows the artist name"
expect_out "● player › resumes after a seek"
expect_out "Tests:       2 failed, 40 passed, 42 total"
expect_out "first failure:"
expect_out "expect(received).toBe(expected)"
expect_out 'Expected: "Ada Lovelace"'
expect_out 'Received: ""'
grep -qF "at frame 1 (node_modules/x.js" "$WORK/out.log" && fail "the first failure block should drop stack frames"
grep -qF "at frame 80 (node_modules/y.js" "$WORK/out.log" && fail "the stack trace tail should not be printed"
[ "$(grep -cF "● library rows › shows the artist name" "$WORK/out.log")" = 2 ] || fail "the first failed test should be named once in the list and once heading its block"
[ "$(grep -cF "● player › resumes after a seek" "$WORK/out.log")" = 1 ] || fail "a later failed test should be named once, in the list only"
grep -qF "● Console" "$WORK/out.log" && fail "console blocks are not failed tests"

CASE="a jest run that never exits is killed at the timeout and fails loud"
new_repo
add_mobile_stubs
add_jest_stub
mobile_src_change
started=$(date +%s)
PRECHECK_JEST_TIMEOUT=2 STUB_JEST=hang NPX_LOG="$WORK/npx.log" run_script_with_stubs
[ $(($(date +%s) - started)) -lt 45 ] || fail "the hung jest run should be killed near the 2s timeout"
expect_rc 1
expect_out "FAIL  mobile tests (related)"
expect_out "jest did not exit within 2s and was killed"

add_thresholded_jest_stub() {
    (
        cd "$WORK/repo" || exit 1
        printf 'module.exports = { coverageThreshold: { "src/shared/apiClient/**": { lines: 100 }, global: { lines: 0 } } };\n' >client/jest.config.js
        git add -A
        git commit -qm "jest config fixture"
        git update-ref refs/remotes/gitea/main HEAD
    )
    cat >"$WORK/bin/npx" <<'STUB'
#!/usr/bin/env bash
printf 'npx %s\n' "$*" >>"$NPX_LOG"
case " $* " in *" jest "*) ;; *) exit 0 ;; esac
case " $* " in
    *" --coverage "*)
        echo "Jest: coverage threshold for lines (100%) not met: 50%"
        exit 1
        ;;
esac
exit 0
STUB
    chmod +x "$WORK/bin/npx"
}

CASE="a change that drops a thresholded file below its threshold turns the mobile jest step red"
new_repo
add_mobile_stubs
add_thresholded_jest_stub
(
    cd "$WORK/repo" || exit 1
    mkdir -p client/src/shared/apiClient
    printf 'export const x = 1;\n' >client/src/shared/apiClient/x.ts
    git add -A
    git commit -qm "thresholded change"
)
NPX_LOG="$WORK/npx.log" run_script_with_stubs
expect_rc 1
expect_out "FAIL  mobile tests (related)"
expect_npx "--collectCoverageFrom=src/shared/apiClient/x.ts"

CASE="a change outside every thresholded glob runs the mobile jest step without coverage"
new_repo
add_mobile_stubs
add_thresholded_jest_stub
(
    cd "$WORK/repo" || exit 1
    mkdir -p client/src/ui
    printf 'export const x = 1;\n' >client/src/ui/x.ts
    git add -A
    git commit -qm "unthresholded change"
)
NPX_LOG="$WORK/npx.log" run_script_with_stubs
expect_out "ok    mobile tests (related)"
grep -qF -- "--coverage" "$WORK/npx.log" && fail "no coverage flag expected outside thresholded globs"

add_migrations() {
    (
        cd "$WORK/repo" || exit 1
        cp "$HERE/migration-prefixes.sh" scripts/
        mkdir -p services/go-api/migrations
        : >services/go-api/migrations/001_base.sql
        git add -A
        git commit -qm "migrations"
        git update-ref refs/remotes/gitea/main HEAD
    )
}

CASE="a new migration taking a number main already used fails precheck"
new_repo
add_migrations
(
    cd "$WORK/repo" || exit 1
    git checkout -q -b feature
    git checkout -q main
    : >services/go-api/migrations/002_from_main.sql
    git add -A
    git commit -qm "main takes 002"
    git update-ref refs/remotes/gitea/main HEAD
    git checkout -q feature
    : >services/go-api/migrations/002_from_feature.sql
    git add -A
    git commit -qm "feature takes 002"
)
run_script
expect_rc 1
expect_out "FAIL  go-api migration numbers are unique"
expect_out "number 002 is taken by more than one migration"

CASE="a new migration with a free number passes the migration check"
new_repo
add_migrations
(
    cd "$WORK/repo" || exit 1
    : >services/go-api/migrations/002_next.sql
    git add -A
    git commit -qm "next migration"
)
run_script
expect_out "ok    go-api migration numbers are unique"

CASE="a change to a repo script runs its self-test"
new_repo
(
    cd "$WORK/repo" || exit 1
    git update-ref refs/remotes/gitea/main HEAD
    printf 'exit 0\n' >scripts/worktree-deps.sh
    printf 'echo ran >"%s/selftest.log"; exit 1\n' "$WORK" >scripts/__tests__/worktree-deps.test.sh
    git add -A
    git commit -qm "script change"
)
run_script
expect_rc 1
expect_out "FAIL  repo script self-tests"
[ -f "$WORK/selftest.log" ] || fail "worktree-deps.test.sh should have run"

CASE="repo script self-tests link the root deps before they run, as a fresh ci worktree has none"
new_repo
(
    cd "$WORK/repo" || exit 1
    git update-ref refs/remotes/gitea/main HEAD
    printf 'mkdir -p node_modules/js-yaml\necho "export PATH=$PATH"\n' >scripts/worktree-deps.sh
    printf 'process.exit(0);\n' >scripts/gate-parity.mjs
    printf '[ -d node_modules/js-yaml ] || { echo "Cannot find package js-yaml"; exit 1; }\n' >scripts/__tests__/gate-parity.test.sh
    git add -A
    git commit -qm "gate parity change"
)
run_script
expect_out "ok    repo script self-tests"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all precheck checks passed\n'
