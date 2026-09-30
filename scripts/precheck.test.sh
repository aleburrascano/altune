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

add_stripcomments_fixture() {
    (
        cd "$WORK/repo" || exit 1
        mkdir -p services/go-api/scripts/stripcomments services/overseer
        printf 'module fixture/go-api\n\ngo 1.26.6\n' >services/go-api/go.mod
        cp "$HERE/../services/go-api/scripts/stripcomments/main.go" services/go-api/scripts/stripcomments/main.go
        cp "$HERE/../services/go-api/scripts/stripcomments/main_test.go" services/go-api/scripts/stripcomments/main_test.go
        printf 'module fixture/overseer\n\ngo 1.26.6\n' >services/overseer/go.mod
        printf 'package main\n\nfunc main() {\n\tprintln("hi")\n}\n' >services/overseer/main.go
        git add -A
        git commit -qm "add stripcomments fixture"
    )
}

build_stripcomments_bin() {
    (
        cd "$WORK/repo/services/go-api" || exit 1
        HEAVY_HELD=1 go build -o "$WORK/stripbin" ./scripts/stripcomments
    ) >/dev/null 2>&1
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

CASE="a real code change to an overseer file that still holds a comment prints ok for the stripcomments self-tests"
new_repo
add_stripcomments_fixture
(
    cd "$WORK/repo" || exit 1
    printf 'package main\n\n// keep this note\nfunc main() {\n\tprintln("hi")\n}\n' >services/overseer/main.go
    git add -A
    git commit -qm "overseer file with a comment"
    git update-ref refs/remotes/gitea/main HEAD
)
(
    cd "$WORK/repo" || exit 1
    printf 'package main\n\n// keep this note\nfunc main() {\n\tprintln("hi again")\n}\n' >services/overseer/main.go
    git add -A
    git commit -qm "real code change, comment untouched"
)
run_script
expect_out "ok    stripcomments self-tests"
expect_out "ok    strip PRs match the strip tool"

CASE="a stripped file committed unchanged from the strip tool's own output passes the strip-matches check"
new_repo
add_stripcomments_fixture
(
    cd "$WORK/repo" || exit 1
    printf 'package widget\n\n// widget does the thing\nfunc widget() int {\n\treturn 1\n}\n' >services/go-api/widget.go
    git add -A
    git commit -qm "add widget with a comment"
    git update-ref refs/remotes/gitea/main HEAD
)
build_stripcomments_bin
(
    cd "$WORK/repo" || exit 1
    "$WORK/stripbin" services/go-api/widget.go
    git add -A
    git commit -qm "strip widget comments"
)
run_script
expect_out "ok    strip PRs match the strip tool"

CASE="hand-editing a stripped file's whitespace only fails the strip-matches check and names the file"
new_repo
add_stripcomments_fixture
(
    cd "$WORK/repo" || exit 1
    printf 'package widget\n\n// widget does the thing\nfunc widget() int {\n\treturn 1\n}\n' >services/go-api/widget.go
    git add -A
    git commit -qm "add widget with a comment"
    git update-ref refs/remotes/gitea/main HEAD
)
build_stripcomments_bin
(
    cd "$WORK/repo" || exit 1
    "$WORK/stripbin" services/go-api/widget.go
    printf '\n' >>services/go-api/widget.go
    git add -A
    git commit -qm "strip then hand-edit whitespace"
)
run_script
expect_rc 1
expect_out "FAIL  strip PRs match the strip tool"
expect_out "services/go-api/widget.go"

CASE="a real code change to an already comment-free Go file is not failed by the strip-matches check"
new_repo
add_stripcomments_fixture
(
    cd "$WORK/repo" || exit 1
    printf 'package widget\n\nfunc widget() int {\n\treturn 1\n}\n' >services/go-api/widget.go
    git add -A
    git commit -qm "comment-free widget"
    git update-ref refs/remotes/gitea/main HEAD
    printf 'package widget\n\nfunc widget() int {\n\treturn 2\n}\n' >services/go-api/widget.go
    git add -A
    git commit -qm "real code change"
)
run_script
expect_out "ok    strip PRs match the strip tool"

CASE="a whitespace-only edit to a Go file that never held comments is not failed by the strip-matches check"
new_repo
add_stripcomments_fixture
(
    cd "$WORK/repo" || exit 1
    printf 'package widget\n\nfunc widget() int {\n\treturn 1\n}\n' >services/go-api/widget.go
    git add -A
    git commit -qm "comment-free widget"
    git update-ref refs/remotes/gitea/main HEAD
    printf 'package widget\n\nfunc widget() int {\n\treturn 1\n}\n\n' >services/go-api/widget.go
    git add -A
    git commit -qm "trailing newline only"
)
run_script
expect_out "ok    strip PRs match the strip tool"

CASE="removing a comment while also adding a new function is treated as a real code change and skipped"
new_repo
add_stripcomments_fixture
(
    cd "$WORK/repo" || exit 1
    printf 'package widget\n\n// widget does the thing\nfunc widget() int {\n\treturn 1\n}\n' >services/go-api/widget.go
    git add -A
    git commit -qm "add widget with a comment"
    git update-ref refs/remotes/gitea/main HEAD
    printf 'package widget\n\nfunc widget() int {\n\treturn 1\n}\n\nfunc widgetTwo() int {\n\treturn 2\n}\n' >services/go-api/widget.go
    git add -A
    git commit -qm "drop comment and add a new function"
)
run_script
expect_out "ok    strip PRs match the strip tool"

CASE="an overseer file stripped and then hand-edited in whitespace fails and names services/overseer/main.go"
new_repo
add_stripcomments_fixture
(
    cd "$WORK/repo" || exit 1
    printf 'package main\n\n// keep this note\nfunc main() {\n\tprintln("hi")\n}\n' >services/overseer/main.go
    git add -A
    git commit -qm "overseer file with a comment"
    git update-ref refs/remotes/gitea/main HEAD
)
build_stripcomments_bin
(
    cd "$WORK/repo" || exit 1
    "$WORK/stripbin" services/overseer/main.go
    printf '\n' >>services/overseer/main.go
    git add -A
    git commit -qm "strip then hand-edit whitespace"
)
run_script
expect_rc 1
expect_out "FAIL  strip PRs match the strip tool"
expect_out "services/overseer/main.go"

CASE="a partial strip that still leaves a comment passes the strip-matches check"
new_repo
add_stripcomments_fixture
(
    cd "$WORK/repo" || exit 1
    printf 'package widget\n\n// keep\n// drop\nfunc widget() int {\n\treturn 1\n}\n' >services/go-api/widget.go
    git add -A
    git commit -qm "add widget with two comments"
    git update-ref refs/remotes/gitea/main HEAD
    printf 'package widget\n\n// keep\nfunc widget() int {\n\treturn 1\n}\n' >services/go-api/widget.go
    git add -A
    git commit -qm "drop one comment, keep the other"
)
run_script
expect_out "ok    strip PRs match the strip tool"

CASE="with two stripped files and one hand-edited, only the hand-edited file is named"
new_repo
add_stripcomments_fixture
(
    cd "$WORK/repo" || exit 1
    printf 'package widget\n\n// widget does the thing\nfunc widget() int {\n\treturn 1\n}\n' >services/go-api/widget.go
    printf 'package main\n\n// keep this note\nfunc main() {\n\tprintln("hi")\n}\n' >services/overseer/main.go
    git add -A
    git commit -qm "add two files with comments"
    git update-ref refs/remotes/gitea/main HEAD
)
build_stripcomments_bin
(
    cd "$WORK/repo" || exit 1
    "$WORK/stripbin" services/go-api/widget.go
    "$WORK/stripbin" services/overseer/main.go
    printf '\n' >>services/overseer/main.go
    git add -A
    git commit -qm "strip both, hand-edit only one"
)
run_script
expect_rc 1
expect_out "FAIL  strip PRs match the strip tool"
expect_out "services/overseer/main.go"
grep "^FAIL  strip PRs match the strip tool" "$WORK/out.log" | grep -q "widget.go" \
    && fail "expected widget.go to not be named in the strip-matches failure"

CASE="deleting a commented Go file does not fail the strip-matches check"
new_repo
add_stripcomments_fixture
(
    cd "$WORK/repo" || exit 1
    printf 'package widget\n\n// widget does the thing\nfunc widget() int {\n\treturn 1\n}\n' >services/go-api/widget.go
    git add -A
    git commit -qm "add widget with a comment"
    git update-ref refs/remotes/gitea/main HEAD
    git rm -q services/go-api/widget.go
    printf 'package main\n\nfunc main() {\n\tprintln("hi there")\n}\n' >services/overseer/main.go
    git add -A
    git commit -qm "delete widget, touch overseer"
)
run_script
expect_out "ok    strip PRs match the strip tool"

CASE="a failing stripcomments self-test prints FAIL and exits 1"
new_repo
add_stripcomments_fixture
(
    cd "$WORK/repo" || exit 1
    git update-ref refs/remotes/gitea/main HEAD
    printf 'package stripcomments\n\nimport "testing"\n\nfunc TestAlwaysFails(t *testing.T) {\n\tt.Fatal("boom")\n}\n' >services/go-api/scripts/stripcomments/always_fails_test.go
    git add -A
    git commit -qm "break the stripcomments self-tests"
)
run_script
expect_rc 1
expect_out "FAIL  stripcomments self-tests"

CASE="--lint skips the stripcomments self-tests but still fails an added shell comment"
new_repo
add_stripcomments_fixture
(
    cd "$WORK/repo" || exit 1
    git update-ref refs/remotes/gitea/main HEAD
    printf 'package stripcomments\n\nimport "testing"\n\nfunc TestAlwaysFails(t *testing.T) {\n\tt.Fatal("boom")\n}\n' >services/go-api/scripts/stripcomments/always_fails_test.go
    git add -A
    git commit -qm "break the stripcomments self-tests"
)
add_commented_shell_file
run_script --lint
expect_rc 1
expect_out "FAIL  workflows and shell no new comments"
grep -q "stripcomments self-tests" "$WORK/out.log" && fail "expected --lint to skip the stripcomments self-tests"

CASE="--lint ignores untracked files, since they are not part of the commit"
new_repo
(
    cd "$WORK/repo" || exit 1
    git update-ref refs/remotes/gitea/main HEAD
    printf '%s\n%s\n' '#!/usr/bin/env bash' '# comment' >untracked.sh
)
run_script --lint
expect_rc 0
expect_out "precheck: lint green, tests left to CI"

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
        mkdir -p apps/mobile
        printf 'x\n' >apps/mobile/.keep
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
    mkdir -p apps/mobile/src
    printf 'export const x = 1;\n' >apps/mobile/src/x.ts
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

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all precheck checks passed\n'
