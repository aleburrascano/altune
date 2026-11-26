#!/usr/bin/env bash
set -uo pipefail

HERE=$(cd "$(dirname "$0")/.." && pwd)
FAILURES=0

new_repo() {
    WORK=$(mktemp -d)
    mkdir -p "$WORK/repo/scripts/__tests__"
    cp "$HERE/precheck.sh" "$WORK/repo/scripts/"
    mkdir -p "$WORK/repo/scripts/ci"
    printf '#!/usr/bin/env bash\nprintf "secret-scan %%s\\n" "$*" >>"%s/tools.log"\nexit "${SECRET_SCAN_STUB_RC:-0}"\n' "$WORK" >"$WORK/repo/scripts/ci/secret-scan.sh"
    (
        cd "$WORK/repo" || exit 1
        git init -qb main
        git config user.email test@example.com
        git config user.name test
        git remote add gitea https://gitea.invalid/altune.git
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
REAL_GO=$(command -v go)
GO_VERSION=$(go env GOVERSION | sed 's/^go//')
forbid_out() {
    ! grep -qF -- "$1" "$WORK/out.log" || fail "expected output not to mention '$1'"
}
expect_log() {
    grep -qF -- "$1" "$WORK/tools.log" 2>/dev/null || fail "expected a tool call including '$1'"
}
forbid_log() {
    ! grep -qF -- "$1" "$WORK/tools.log" 2>/dev/null || fail "expected no tool call including '$1'"
}
commit_as_base() {
    (
        cd "$WORK/repo" || exit 1
        git add -A
        git commit -qm "base"
        git update-ref refs/remotes/gitea/main HEAD
    )
}
add_go_fixture() {
    mkdir -p "$WORK/bin" "$WORK/home"
    cat >"$WORK/bin/go" <<STUB
#!/usr/bin/env bash
printf "go %s\\n" "\$*" >>"$WORK/tools.log"
case "\$*" in
"run golang.org/x/tools/cmd/deadcode@"*) printf "%s" "\${DEADCODE_STUB_OUT:-}"; exit "\${DEADCODE_STUB_RC:-0}" ;;
esac
exec "$REAL_GO" "\$@"
STUB
    printf '#!/usr/bin/env bash\nprintf "golangci-lint %%s\\n" "$*" >>"%s/tools.log"\nprintf "golangci-lint-cache %%s %%s\\n" "$1" "$GOLANGCI_LINT_CACHE" >>"%s/tools.log"\n' "$WORK" "$WORK" >"$WORK/bin/golangci-lint"
    printf '#!/usr/bin/env bash\nprintf "govulncheck %%s\\n" "$*" >>"%s/tools.log"\n' "$WORK" >"$WORK/bin/govulncheck"
    chmod +x "$WORK/bin/go" "$WORK/bin/golangci-lint" "$WORK/bin/govulncheck"
    mkdir -p "$WORK/repo/scripts/ci"
    cp "$HERE/ci/go-deadcode.sh" "$WORK/repo/scripts/ci/"
    (
        cd "$WORK/repo" || exit 1
        api=services/go-api
        mkdir -p $api/internal/shared/s $api/internal/acq $api/internal/discovery/adapters/x $api/cmd/acquisitioneval
        printf 'module altune/go-api\n\ngo %s\n' "$GO_VERSION" >$api/go.mod
        printf 'package s\n\nfunc S() int { return 1 }\n' >$api/internal/shared/s/s.go
        printf 'package acq\n\nimport "altune/go-api/internal/shared/s"\n\nfunc A() int { return s.S() }\n' >$api/internal/acq/a.go
        printf 'package x\n\nfunc X() int { return 1 }\n' >$api/internal/discovery/adapters/x/x.go
        printf 'package main\n\nimport "altune/go-api/internal/acq"\n\nfunc main() { _ = acq.A() }\n' >$api/cmd/acquisitioneval/main.go
    )
    commit_as_base
}
edit_go_file() {
    (
        cd "$WORK/repo" || exit 1
        printf '\nvar edited = 1\n' >>"services/go-api/$1"
        git add -A
        git commit -qm "edit $1"
    )
}
run_with_tools() {
    (
        cd "$WORK/repo" || exit 1
        HOME="$WORK/home" PATH="$WORK/bin:$PATH" NPX_LOG="$WORK/npx.log" bash scripts/precheck.sh "$@" >"$WORK/out.log" 2>&1
    )
    RC=$?
}
stage_go_file() {
    (
        cd "$WORK/repo" || exit 1
        printf '\nvar edited = 1\n' >>"services/go-api/$1"
        git add -A
    )
}
CASE="a go-api change lints the changed package and its importers, not ./..."
new_repo
add_go_fixture
edit_go_file internal/shared/s/s.go
run_with_tools
expect_log "golangci-lint run --config .golangci.strict.yml --new-from-rev="
expect_log "altune/go-api/internal/shared/s altune/go-api/internal/acq altune/go-api/cmd/acquisitioneval"
forbid_log "--allow-serial-runners ./..."

CASE="a change to go.mod lints the whole module"
new_repo
add_go_fixture
(
    cd "$WORK/repo" || exit 1
    printf '\nexclude example.invalid/none v1.0.0\n' >>services/go-api/go.mod
    git add -A
    git commit -qm "touch go.mod"
)
run_with_tools
expect_log "--allow-serial-runners ./..."

CASE="PRECHECK_LINT_WHOLE=1 lints the whole module for a one-package change"
new_repo
add_go_fixture
edit_go_file internal/discovery/adapters/x/x.go
PRECHECK_LINT_WHOLE=1 run_with_tools
expect_log "--allow-serial-runners ./..."

add_npm_stub() {
    printf '#!/usr/bin/env bash\nprintf "npm %%s\\n" "$*" >>"%s/tools.log"\n' "$WORK" >"$WORK/bin/npm"
    chmod +x "$WORK/bin/npm"
}
add_contract_tests() {
    (
        cd "$WORK/repo" || exit 1
        mkdir -p client/src/shared/a/__tests__ client/src/shared/b/__tests__
        printf "const dir = 'go-api';\n" >client/src/shared/a/__tests__/aContract.test.ts
        printf "const dir = 'go-api';\n" >client/src/shared/b/__tests__/bContract.test.tsx
        printf "const dir = 'elsewhere';\n" >client/src/shared/b/__tests__/other.test.ts
        printf "const dir = 'go-api';\n" >client/src/shared/b/notATest.ts
    )
    commit_as_base
}
CASE="a green full precheck is remembered for the same tree, so a second run does not run the tools"
new_repo
printf '.cache/\n' >"$WORK/repo/.gitignore"
commit_as_base
add_plain_file
run_with_tools
expect_rc 0
expect_out "precheck: green"
: >"$WORK/tools.log"
run_with_tools
expect_rc 0
expect_out "already proven for this exact tree"
[ ! -s "$WORK/tools.log" ] || fail "a cached green must not run any tool: $(cat "$WORK/tools.log")"

CASE="PRECHECK_NO_CACHE=1 reruns everything"
new_repo
printf '.cache/\n' >"$WORK/repo/.gitignore"
commit_as_base
add_plain_file
run_with_tools
PRECHECK_NO_CACHE=1 run_with_tools
expect_out "precheck: green"
grep -qF "already proven" "$WORK/out.log" && fail "PRECHECK_NO_CACHE=1 must not use the cache"

CASE="a red precheck is never remembered"
new_repo
printf '.cache/\n' >"$WORK/repo/.gitignore"
commit_as_base
add_plain_file
SECRET_SCAN_STUB_RC=1 run_with_tools
expect_rc 1
run_with_tools
grep -qF "already proven" "$WORK/out.log" && fail "a red run must not be cached"

CASE="a different tree is not served from the cache"
new_repo
printf '.cache/\n' >"$WORK/repo/.gitignore"
commit_as_base
add_plain_file
run_with_tools
(
    cd "$WORK/repo" || exit 1
    printf 'more\n' >>added.txt
    git add -A
    git commit -qm "change the tree"
)
run_with_tools
grep -qF "already proven" "$WORK/out.log" && fail "a changed tree must rerun the checks"

cached_green_repo() {
    new_repo
    printf '.cache/\n' >"$WORK/repo/.gitignore"
    commit_as_base
    add_plain_file
    mkdir -p "$WORK/bin"
    run_with_tools
}
expect_not_cached() {
    grep -qF "already proven" "$WORK/out.log" && fail "$1"
    expect_out "precheck: green"
}
CASE="uncommitted changes are never served from the cache"
new_repo
printf '.cache/\n' >"$WORK/repo/.gitignore"
commit_as_base
add_plain_file
run_with_tools
printf 'dirty\n' >>"$WORK/repo/added.txt"
run_with_tools
grep -qF "already proven" "$WORK/out.log" && fail "a dirty tree must rerun the checks"



CASE="a base ref that moved forward since the green run is not served from the cache"
cached_green_repo
(
    cd "$WORK/repo" || exit 1
    base_sha=$(git rev-parse gitea/main)
    git checkout -q -b side "$base_sha"
    printf 'newer\n' >newer.txt
    git add -A
    git commit -qm "newer main"
    git update-ref refs/remotes/gitea/main HEAD
    git checkout -q main
)
run_with_tools
expect_not_cached "a moved base ref must rerun the checks, since some read its tip"

CASE="a different INTEGRATION value is not served from the cache"
cached_green_repo
INTEGRATION=1 run_with_tools
expect_not_cached "INTEGRATION changes which checks run"

CASE="a different PRECHECK_LINT_WHOLE value is not served from the cache"
cached_green_repo
PRECHECK_LINT_WHOLE=1 run_with_tools
expect_not_cached "PRECHECK_LINT_WHOLE changes what is linted"

CASE="a different CLIENT_MUTATE_RUNS_ELSEWHERE value is not served from the cache"
cached_green_repo
CLIENT_MUTATE_RUNS_ELSEWHERE=1 run_with_tools
expect_not_cached "CLIENT_MUTATE_RUNS_ELSEWHERE changes which checks run"

CASE="a different tool version is not served from the cache"
new_repo
printf '.cache/\n' >"$WORK/repo/.gitignore"
commit_as_base
add_plain_file
mkdir -p "$WORK/bin"
printf '#!/bin/sh\necho "ShellCheck version: one"\n' >"$WORK/bin/shellcheck"
chmod +x "$WORK/bin/shellcheck"
run_with_tools
printf '#!/bin/sh\necho "ShellCheck version: two"\n' >"$WORK/bin/shellcheck"
run_with_tools
expect_not_cached "a tool version change must rerun the checks"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all precheck checks passed\n'
