#!/usr/bin/env bash
set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
FAILURES=0

new_repo() {
    WORK=$(mktemp -d)
    mkdir -p "$WORK/bin" "$WORK/repo/services/go-api" "$WORK/repo/other"
    cp "$HERE/fmt-changed.sh" "$WORK/repo/services/go-api/"
    (
        cd "$WORK/repo" || exit 1
        git init -qb main
        git config user.email test@example.com
        git config user.name test
    )
}

install_stub() {
    : >"$WORK/calls.log"
    cat >"$WORK/bin/golangci-lint" <<EOF
#!/usr/bin/env bash
printf '%s\n' "\$*" >>"$WORK/calls.log"
[ "\${STUB_DIFF:-0}" = 1 ] && [[ " \$* " == *" --diff "* ]] && exit 1
exit 0
EOF
    chmod +x "$WORK/bin/golangci-lint"
}

run_script() {
    (
        cd "$WORK/repo/services/go-api" || exit 1
        PATH="$WORK/bin:$PATH" bash fmt-changed.sh "$@" >"$WORK/out.log" 2>&1
    )
    RC=$?
}

setup_case_with_touched_files() {
    new_repo
    (
        cd "$WORK/repo" || exit 1
        printf 'package keep\n' >services/go-api/keep.go
        printf 'package changed\n' >services/go-api/changed.go
        printf 'package other\n' >other/changed.go
        git add -A
        git commit -qm baseline
        git checkout -qb feature
        printf 'package changed\n\nfunc x() {}\n' >services/go-api/changed.go
        printf 'package other\n\nfunc y() {}\n' >other/changed.go
        git add services/go-api/changed.go other/changed.go
        printf 'package untracked\n' >services/go-api/untracked.go
    )
    install_stub
    run_script "$@"
}

setup_case_no_changes() {
    new_repo
    (
        cd "$WORK/repo" || exit 1
        printf 'package keep\n' >services/go-api/keep.go
        git add -A
        git commit -qm baseline
        git checkout -qb feature
    )
    install_stub
    run_script "$@"
}

setup_case_missing_golangci() {
    new_repo
    (
        cd "$WORK/repo" || exit 1
        printf 'package keep\n' >services/go-api/keep.go
        git add -A
        git commit -qm baseline
        git checkout -qb feature
        printf 'package keep\n\nfunc z() {}\n' >services/go-api/keep.go
    )
    (
        cd "$WORK/repo/services/go-api" || exit 1
        PATH="/usr/bin:/bin" bash fmt-changed.sh "$@" >"$WORK/out.log" 2>&1
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

file_args_passed_to_stub() {
    local call
    call=$(cat "$WORK/calls.log")
    tr ' ' '\n' <<<"$call" | grep -vE '^(fmt|-c|--diff|\.golangci\.strict\.yml)$' | sort
}

CASE="passes exactly the changed and untracked go files, never an untouched file or a directory"
setup_case_with_touched_files main
expect_rc 0
call=$(cat "$WORK/calls.log")
[[ "$call" == *"-c .golangci.strict.yml"* ]] || fail "expected the strict config flag, got: $call"
got=$(file_args_passed_to_stub)
want=$(printf '%s\n' changed.go untracked.go | sort)
[ "$got" = "$want" ] || fail "expected exactly {changed.go, untracked.go}, got: $(tr '\n' ' ' <<<"$got")"

CASE="--check exits 1 when the stub reports a diff"
STUB_DIFF=1 setup_case_with_touched_files main --check
expect_rc 1
call=$(cat "$WORK/calls.log")
[[ "$call" == *"--diff"* ]] || fail "expected --diff among the arguments under --check, got: $call"

CASE="--check exits 0 when the stub reports no diff"
setup_case_with_touched_files main --check
expect_rc 0

CASE="no changed go files exits 0 and reports nothing to format"
setup_case_no_changes main
expect_rc 0
expect_out "fmt-changed: nothing to format"
[ -s "$WORK/calls.log" ] && fail "called golangci-lint despite no changed files"

CASE="missing golangci-lint exits 3"
setup_case_missing_golangci main
expect_rc 3

install_arg_stub() {
    : >"$WORK/args.log"
    cat >"$WORK/bin/golangci-lint" <<EOF
#!/usr/bin/env bash
for a in "\$@"; do
    case "\$a" in fmt|-c|--diff|.golangci.strict.yml) continue ;; esac
    if [ -f "\$a" ]; then printf '%s\n' "\$a" >>"$WORK/args.log"; else printf 'NOT-A-FILE:%s\n' "\$a" >>"$WORK/args.log"; fi
done
exit 0
EOF
    chmod +x "$WORK/bin/golangci-lint"
}

expect_args() {
    local got want
    got=$(sort "$WORK/args.log")
    want=$(printf '%s\n' "$@" | sort)
    [ "$got" = "$want" ] || fail "expected exactly {$*}, got: $(tr '\n' ' ' <<<"$got")"
}

CASE="committed, unstaged and nested changes are passed as existing file paths from services/go-api"
new_repo
(
    cd "$WORK/repo" || exit 1
    mkdir -p services/go-api/internal/deep/pkg
    printf 'package pkg\n' >services/go-api/internal/deep/pkg/committed.go
    printf 'package pkg\n' >services/go-api/internal/deep/pkg/unstaged.go
    printf 'package pkg\n' >services/go-api/internal/deep/pkg/untouched.go
    git add -A
    git commit -qm baseline
    git checkout -qb feature
    printf 'package pkg\n\nfunc a() {}\n' >services/go-api/internal/deep/pkg/committed.go
    git commit -qam "branch commit"
    printf 'package pkg\n\nfunc b() {}\n' >services/go-api/internal/deep/pkg/unstaged.go
    mkdir -p services/go-api/internal/fresh
    printf 'package fresh\n' >services/go-api/internal/fresh/new.go
)
install_arg_stub
run_script main
expect_rc 0
expect_args internal/deep/pkg/committed.go internal/deep/pkg/unstaged.go internal/fresh/new.go

CASE="deleted go files and non-go changes are not passed; a renamed file is passed by its new path"
new_repo
(
    cd "$WORK/repo" || exit 1
    printf 'package gone\n' >services/go-api/gone.go
    printf 'package old\n' >services/go-api/old_name.go
    printf 'module x\n' >services/go-api/go.mod
    printf 'notes\n' >services/go-api/README.md
    git add -A
    git commit -qm baseline
    git checkout -qb feature
    git rm -q services/go-api/gone.go
    git mv services/go-api/old_name.go services/go-api/new_name.go
    printf 'module x\n\ngo 1.22\n' >services/go-api/go.mod
    printf 'more notes\n' >services/go-api/README.md
    printf 'not go\n' >services/go-api/data.go.txt
    git add -A
    git commit -qm "branch changes"
)
install_arg_stub
run_script main
expect_rc 0
expect_args new_name.go

CASE="go files changed on the base after the branch point are not passed"
new_repo
(
    cd "$WORK/repo" || exit 1
    printf 'package base\n' >services/go-api/base.go
    printf 'package mine\n' >services/go-api/mine.go
    git add -A
    git commit -qm baseline
    git checkout -qb feature
    printf 'package mine\n\nfunc m() {}\n' >services/go-api/mine.go
    git commit -qam "branch change"
    git checkout -q main
    printf 'package base\n\nfunc later() {}\n' >services/go-api/base.go
    printf 'package landed\n' >services/go-api/landed.go
    git add -A
    git commit -qm "landed on main after the branch point"
    git checkout -q feature
)
install_arg_stub
run_script main
expect_rc 0
expect_args mine.go

CASE="--check alone uses the default base origin/main instead of treating --check as a ref"
new_repo
(
    cd "$WORK/repo" || exit 1
    printf 'package keep\n' >services/go-api/keep.go
    git add -A
    git commit -qm baseline
    git update-ref refs/remotes/origin/main HEAD
    git checkout -qb feature
    printf 'package keep\n\nfunc c() {}\n' >services/go-api/keep.go
    git commit -qam "branch change"
)
install_stub
STUB_DIFF=1 run_script --check
expect_rc 1
call=$(cat "$WORK/calls.log")
[[ "$call" == *"--diff"* ]] || fail "expected --diff under --check with the default base, got: $call"
got=$(file_args_passed_to_stub)
[ "$got" = "keep.go" ] || fail "expected exactly {keep.go}, got: $(tr '\n' ' ' <<<"$got")"

CASE="--check with no changed go files exits 0 and reports nothing to format"
STUB_DIFF=1 setup_case_no_changes main --check
expect_rc 0
expect_out "fmt-changed: nothing to format"
[ -s "$WORK/calls.log" ] && fail "called golangci-lint despite no changed files"

CASE="default picks gitea/main when both gitea/main and origin/main exist"
new_repo
(
    cd "$WORK/repo" || exit 1
    printf 'package keep\n' >services/go-api/keep.go
    git add -A
    git commit -qm baseline
    git update-ref refs/remotes/gitea/main HEAD
    git checkout -q --orphan orphan
    git commit -q --allow-empty -m unrelated
    git update-ref refs/remotes/origin/main HEAD
    git checkout -q main
    git checkout -qb feature
    printf 'package keep\n\nfunc c() {}\n' >services/go-api/keep.go
    git commit -qam "branch change"
)
install_arg_stub
run_script
expect_rc 0
expect_args keep.go

CASE="falls back to origin/main when gitea/main is absent"
new_repo
(
    cd "$WORK/repo" || exit 1
    printf 'package keep\n' >services/go-api/keep.go
    git add -A
    git commit -qm baseline
    git update-ref refs/remotes/origin/main HEAD
    git checkout -qb feature
    printf 'package keep\n\nfunc c() {}\n' >services/go-api/keep.go
    git commit -qam "branch change"
)
install_arg_stub
run_script
expect_rc 0
expect_args keep.go

CASE="missing base (neither gitea/main nor origin/main) exits 2"
new_repo
(
    cd "$WORK/repo" || exit 1
    printf 'package keep\n' >services/go-api/keep.go
    git add -A
    git commit -qm baseline
    git checkout -qb feature
    printf 'package keep\n\nfunc c() {}\n' >services/go-api/keep.go
    git commit -qam "branch change"
)
install_stub
run_script
expect_rc 2

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all fmt-changed checks passed\n'
