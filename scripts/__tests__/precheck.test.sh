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

CASE="a gitea/main left over from a removed gitea remote is ignored, so the default base is origin/main"
new_repo
(
    cd "$WORK/repo" || exit 1
    git update-ref refs/remotes/origin/main HEAD
)
add_failing_nocomments_check
(
    cd "$WORK/repo" || exit 1
    git update-ref refs/remotes/gitea/main HEAD
    git remote remove gitea
    git update-ref refs/remotes/gitea/main HEAD
)
run_script
expect_rc 1
expect_out "bash scripts/precheck.sh origin/main"

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

CASE="a changed client script outside src is linted too"
new_repo
add_mobile_stubs
(
    cd "$WORK/repo" || exit 1
    mkdir -p client/scripts
    printf 'export const x = 1;\n' >client/scripts/tool.mjs
    git add -A
    git commit -qm "client script change"
)
run_script_with_stubs
expect_npx "eslint scripts/tool.mjs"

CASE="mobile contract tests run by kind and keep the jest cache under HOME, not the /tmp tmpfs"
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
expect_npx "\\.contract\\.test\\.tsx?\$"
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
[ "$(sed '/^precheck: red/q' "$WORK/out.log" | grep -cF "● library rows › shows the artist name")" = 2 ] || fail "the first failed test should be named once in the list and once heading its block"
[ "$(sed '/^precheck: red/q' "$WORK/out.log" | grep -cF "● player › resumes after a seek")" = 1 ] || fail "a later failed test should be named once, in the list only"
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

CASE="a mobile source change runs the related jest step with no coverage gate"
new_repo
add_mobile_stubs
mobile_src_change
NPX_LOG="$WORK/npx.log" run_script_with_stubs
expect_out "ok    mobile tests (related)"
grep -qF -- "--coverage" "$WORK/npx.log" && fail "no coverage flag expected: mutation testing is the test-strength check"

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

CASE="a change to Stryker's client config runs the client mutation self-test"
new_repo
(
    cd "$WORK/repo" || exit 1
    mkdir -p client scripts/ci/__tests__
    printf 'exit 0\n' >scripts/worktree-deps.sh
    printf 'echo ran >"%s/selftest.log"; exit 1\n' "$WORK" >scripts/ci/__tests__/client-mutate.test.sh
    printf '{}\n' >client/stryker.config.json
    git add -A
    git commit -qm "client mutation self-test"
    git update-ref refs/remotes/gitea/main HEAD
    printf '{"logLevel":"info"}\n' >client/stryker.config.json
    git commit -qam "stryker config change"
)
run_script
expect_out "FAIL  repo script self-tests"
[ -f "$WORK/selftest.log" ] || fail "client-mutate.test.sh should have run"

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

REAL_GO=$(command -v go)
GO_VERSION=$(go env GOVERSION | sed 's/^go//')
export GOCACHE=${GOCACHE:-$(go env GOCACHE)}
export GOPATH=$(go env GOPATH)

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

CASE="a go-api change in a package the selection gate does not import skips the gate, vets only that package, and still runs govulncheck"
new_repo
add_go_fixture
edit_go_file internal/discovery/adapters/x/x.go
run_with_tools
forbid_log "run ./cmd/acquisitioneval"
expect_log "govulncheck ./..."
expect_log "vet altune/go-api/internal/discovery/adapters/x"
forbid_log "vet ./..."

stage_go_file() {
    (
        cd "$WORK/repo" || exit 1
        printf '\nvar edited = 1\n' >>"services/go-api/$1"
        git add -A
    )
}

CASE="--lint (the pre-commit hook) leaves govulncheck and vet to the push stage"
new_repo
add_go_fixture
stage_go_file internal/discovery/adapters/x/x.go
run_with_tools --lint
forbid_log "govulncheck"
forbid_log "vet "
expect_log "golangci-lint-cache run"
expect_log "--fast-only"

CASE="--lint lints a Go edit that is not staged, as in git commit -a, where git hands the hook a temporary index"
new_repo
add_go_fixture
(
    cd "$WORK/repo" || exit 1
    printf '\nvar edited = 1\n' >>services/go-api/internal/discovery/adapters/x/x.go
)
run_with_tools --lint
expect_log "golangci-lint-cache run"
expect_log "--fast-only"

CASE="a Go-only commit installs and touches no client dependencies and runs no jest"
new_repo
add_go_fixture
printf '#!/usr/bin/env bash\necho "worktree-deps $*" >>"%s/deps.log"\necho true\n' "$WORK" >"$WORK/repo/scripts/worktree-deps.sh"
mkdir -p "$WORK/repo/client/src/shared/a/__tests__"
printf "// 'go-api'\n" >"$WORK/repo/client/src/shared/a/__tests__/aContract.test.ts"
git -C "$WORK/repo" add -A
git -C "$WORK/repo" commit -qm "client fixture and deps stub"
git -C "$WORK/repo" update-ref refs/remotes/gitea/main HEAD
stage_go_file internal/discovery/adapters/x/x.go
: >"$WORK/deps.log"
run_with_tools --lint
[ ! -s "$WORK/deps.log" ] || fail "a Go-only commit must not call worktree-deps: $(cat "$WORK/deps.log")"
[ ! -s "$WORK/npx.log" ] || fail "a Go-only commit must not run npx: $(cat "$WORK/npx.log")"
expect_log "golangci-lint-cache run"

CASE="a commit that touches no workflow file does not run gate parity or the duplicate-CI check, so it needs no root node_modules"
new_repo
add_go_fixture
printf '#!/usr/bin/env bash\necho "worktree-deps $*" >>"%s/deps.log"\necho true\n' "$WORK" >"$WORK/repo/scripts/worktree-deps.sh"
printf 'process.exit(1);\n' >"$WORK/repo/scripts/gate-parity.mjs"
git -C "$WORK/repo" add -A
git -C "$WORK/repo" commit -qm "deps stub"
git -C "$WORK/repo" update-ref refs/remotes/gitea/main HEAD
stage_go_file internal/discovery/adapters/x/x.go
: >"$WORK/deps.log"
run_with_tools --lint
grep -qF "gate parity" "$WORK/out.log" && fail "gate parity must not run for a Go-only commit"
[ ! -s "$WORK/deps.log" ] || fail "no dependencies may be linked for a Go-only commit: $(cat "$WORK/deps.log")"

CASE="--lint lints nothing when the working tree matches HEAD"
new_repo
add_go_fixture
run_with_tools --lint
forbid_log "golangci-lint"

CASE="a go-api change runs deadcode, which reports nothing"
new_repo
add_go_fixture
edit_go_file internal/discovery/adapters/x/x.go
run_with_tools
expect_log "go run golang.org/x/tools/cmd/deadcode@v0.53.0 -test ./..."
grep -q "FAIL  go-api deadcode" "$WORK/out.log" && fail "deadcode with no findings must pass"

CASE="a function deadcode finds fails the check and names it"
new_repo
add_go_fixture
edit_go_file internal/discovery/adapters/x/x.go
DEADCODE_STUB_OUT='x/x.go:5:6: unreachable func: Orphan' run_with_tools
grep -q "FAIL  go-api deadcode" "$WORK/out.log" || fail "a deadcode finding must fail the check"
grep -qF "unreachable func: Orphan" "$WORK/out.log" || fail "the failure must show the finding"
[ "$RC" != 0 ] || fail "a deadcode finding must make precheck exit non-zero"

CASE="a deadcode run that fails outright is not mistaken for a clean tree"
new_repo
add_go_fixture
edit_go_file internal/discovery/adapters/x/x.go
DEADCODE_STUB_RC=1 run_with_tools
grep -q "FAIL  go-api deadcode" "$WORK/out.log" || fail "a tool failure must fail the check"
grep -qF "so nothing was checked" "$WORK/out.log" || fail "the failure must say nothing was checked"

CASE="--lint (the pre-commit hook) leaves deadcode to CI"
new_repo
add_go_fixture
edit_go_file internal/discovery/adapters/x/x.go
run_with_tools --lint
forbid_log "deadcode"

CASE="without INTEGRATION=1 a go-api change skips the mutation step and names the job that runs it"
new_repo
add_go_fixture
edit_go_file internal/discovery/adapters/x/x.go
run_with_tools
grep -qF "SKIP  go-api mutation (changed lines): integration tests cover lines here, so it needs INTEGRATION=1 and a database; the backend-tests CI job runs it" "$WORK/out.log" || fail "expected the mutation step to skip and name backend-tests"

CASE="with INTEGRATION=1 a go-api change runs the mutation step"
new_repo
add_go_fixture
edit_go_file internal/discovery/adapters/x/x.go
INTEGRATION=1 run_with_tools
grep -qE "^(ok|FAIL)  go-api mutation \(changed lines\)" "$WORK/out.log" || fail "expected the mutation step to run"

CASE="a go-api change in a package the selection gate imports runs the gate and vets every importer"
new_repo
add_go_fixture
edit_go_file internal/shared/s/s.go
run_with_tools
expect_log "run ./cmd/acquisitioneval"
expect_log "govulncheck ./..."
expect_log "vet altune/go-api/internal/shared/s altune/go-api/internal/acq altune/go-api/cmd/acquisitioneval"

CASE="a change to the selection gate command runs the gate"
new_repo
add_go_fixture
edit_go_file cmd/acquisitioneval/main.go
run_with_tools
expect_log "run ./cmd/acquisitioneval"

CASE="a change to go.mod runs govulncheck, the whole-module vet and the selection gate"
new_repo
add_go_fixture
(
    cd "$WORK/repo" || exit 1
    printf '\nexclude example.invalid/none v1.0.0\n' >>services/go-api/go.mod
    git add -A
    git commit -qm "touch go.mod"
)
run_with_tools
expect_log "govulncheck ./..."
expect_log "vet ./..."
expect_log "run ./cmd/acquisitioneval"

CASE="a change to go.sum runs govulncheck"
new_repo
add_go_fixture
(
    cd "$WORK/repo" || exit 1
    : >services/go-api/go.sum
    git add -A
    git commit -qm "add go.sum"
)
run_with_tools
expect_log "govulncheck ./..."

CASE="a deleted go file still vets its package and the packages that import it"
new_repo
add_go_fixture
(
    cd "$WORK/repo" || exit 1
    printf 'package s\n\nfunc T() int { return 2 }\n' >services/go-api/internal/shared/s/t.go
    git add -A
    git commit -qm "second file"
    git update-ref refs/remotes/gitea/main HEAD
    git rm -q services/go-api/internal/shared/s/t.go
    printf 'notes\n' >services/go-api/NOTES.md
    git add -A
    git commit -qm "delete second file"
)
run_with_tools
expect_log "vet altune/go-api/internal/shared/s altune/go-api/internal/acq"
forbid_log "vet ./..."

CASE="a go file that no longer parses fails the scoped vet"
new_repo
add_go_fixture
(
    cd "$WORK/repo" || exit 1
    printf 'package x\n\nfunc broken( {\n' >services/go-api/internal/discovery/adapters/x/x.go
    git add -A
    git commit -qm "break x"
)
run_with_tools
expect_rc 1
expect_out "FAIL  go-api vet"
expect_log "vet altune/go-api/internal/discovery/adapters/x"

CASE="an overseer change skips its govulncheck unless go.mod or go.sum changed"
new_repo
add_go_fixture
(
    cd "$WORK/repo" || exit 1
    mkdir -p services/overseer
    printf 'module altune/overseer\n\ngo %s\n' "$GO_VERSION" >services/overseer/go.mod
    printf 'package main\n\nfunc main() {}\n' >services/overseer/main.go
)
commit_as_base
(
    cd "$WORK/repo" || exit 1
    printf '\nvar edited = 1\n' >>services/overseer/main.go
    git add -A
    git commit -qm "edit overseer"
)
run_with_tools
forbid_log "govulncheck"
(
    cd "$WORK/repo" || exit 1
    : >services/overseer/go.sum
    git add -A
    git commit -qm "overseer go.sum"
)
run_with_tools
expect_log "govulncheck ./..."

add_npm_stub() {
    printf '#!/usr/bin/env bash\nprintf "npm %%s\\n" "$*" >>"%s/tools.log"\n' "$WORK" >"$WORK/bin/npm"
    chmod +x "$WORK/bin/npm"
}

CASE="a client change runs react-doctor and fallow over the whole client"
new_repo
add_mobile_stubs
add_npm_stub
(
    cd "$WORK/repo" || exit 1
    mkdir -p client/src
    printf 'export const x = 1;\n' >client/src/x.ts
    git add -A
    git commit -qm "client change"
)
run_script_with_stubs
expect_log "npm run --silent react-doctor"
expect_log "npm run --silent fallow"
expect_out "mobile boundaries and duplication (changed files)"
expect_out "mobile mutation (changed lines)"

CASE="a CI job that runs client mutation itself skips it in precheck"
CLIENT_MUTATE_RUNS_ELSEWHERE=1 run_script_with_stubs
expect_out "SKIP  mobile mutation (changed lines): the mobile-mutation CI job runs it"

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

CASE="a go-api change runs the client test files that mention go-api, found at run time"
new_repo
add_mobile_stubs
add_jest_stub
add_contract_tests
add_go_fixture
edit_go_file internal/discovery/adapters/x/x.go
run_with_tools
expect_out "ok    mobile contract tests (go-api changed)"
expect_npx "src/shared/a/__tests__/aContract.test.ts src/shared/b/__tests__/bContract.test.tsx"
grep -qF "other.test.ts" "$WORK/npx.log" && fail "a test file that does not mention go-api should not run"
grep -qF "notATest.ts" "$WORK/npx.log" && fail "a non-test file that mentions go-api should not run"

CASE="a go-api change that breaks a client contract test fails precheck"
new_repo
add_mobile_stubs
add_jest_stub
add_contract_tests
add_go_fixture
edit_go_file internal/discovery/adapters/x/x.go
STUB_JEST=fail run_with_tools
expect_rc 1
expect_out "FAIL  mobile contract tests (go-api changed)"

CASE="a client-only change does not run the contract tests"
new_repo
add_mobile_stubs
add_jest_stub
add_contract_tests
mobile_src_change
run_with_tools
forbid_out "mobile contract tests"

CASE="a client with no test file mentioning go-api fails the contract step loudly"
new_repo
add_mobile_stubs
add_jest_stub
add_go_fixture
(
    cd "$WORK/repo" || exit 1
    mkdir -p client/src
    printf 'export const a = 1;\n' >client/src/a.ts
)
commit_as_base
edit_go_file internal/discovery/adapters/x/x.go
run_with_tools
expect_rc 1
expect_out "FAIL  mobile contract tests (go-api changed): no client test file mentions 'go-api'"

CASE="a change that only deletes a go-api go file still vets and tests the package it left"
new_repo
add_go_fixture
(
    cd "$WORK/repo" || exit 1
    printf 'package x\n\nfunc Y() int { return 1 }\n' >services/go-api/internal/discovery/adapters/x/y.go
)
commit_as_base
(
    cd "$WORK/repo" || exit 1
    git rm -q services/go-api/internal/discovery/adapters/x/y.go
    git commit -qm "delete y"
)
run_with_tools
expect_log "vet altune/go-api/internal/discovery/adapters/x"
expect_log "test -race -shuffle=on -count=1 ./internal/discovery/adapters/x"

CASE="every golangci-lint call runs with a cache inside the checkout"
new_repo
add_go_fixture
stage_go_file internal/discovery/adapters/x/x.go
run_with_tools --lint
expect_log "golangci-lint-cache run $WORK/repo/.cache/golangci-lint"
stray=$(grep "^golangci-lint-cache run " "$WORK/tools.log" | grep -vF " $WORK/repo/.cache/golangci-lint")
[ -z "$stray" ] || fail "a golangci-lint call ran outside the checkout cache"

CASE="every change is scanned for secrets from the merge base to HEAD"
new_repo
commit_as_base
add_plain_file
run_with_tools
expect_log "secret-scan $(git -C "$WORK/repo" rev-parse gitea/main)"
expect_out "ok    secret scan (commits since base)"

CASE="a secret found in the range fails precheck"
new_repo
commit_as_base
add_plain_file
SECRET_SCAN_STUB_RC=1 run_with_tools
expect_out "FAIL  secret scan (commits since base)"
[ "$RC" != 0 ] || fail "a secret scan finding must make precheck exit non-zero"

CASE="--lint (the pre-commit hook) scans the uncommitted changes for secrets"
new_repo
commit_as_base
add_plain_file
run_with_tools --lint
expect_log "secret-scan --pending"
forbid_log "secret-scan $(git -C "$WORK/repo" rev-parse gitea/main)"

CASE="a step that fails with no output is still named, with its exit code, in the last lines"
new_repo
commit_as_base
add_plain_file
SECRET_SCAN_STUB_RC=1 run_with_tools --lint
expect_rc 1
expect_out "FAIL secret scan (pending changes) (exit 1):"
expect_out "(no output; exit 1)"
[ "$(tail -n 2 "$WORK/out.log" | head -n 1)" = "FAIL secret scan (pending changes) (exit 1):" ] \
    || fail "the failing step is not named in the last lines: $(tail -n 2 "$WORK/out.log" | tr '\n' '|')"

CASE="a failing step's first output lines are repeated in the last lines"
new_repo
commit_as_base
add_plain_file
printf '#!/usr/bin/env bash\necho "leaked something at x.txt:3"\necho "second line"\nexit 1\n' >"$WORK/repo/scripts/ci/secret-scan.sh"
run_with_tools --lint
expect_rc 1
[ "$(tail -n 3 "$WORK/out.log" | head -n 1)" = "FAIL secret scan (pending changes) (exit 1):" ] \
    || fail "summary header is not in the last lines: $(tail -n 3 "$WORK/out.log" | tr '\n' '|')"
grep -qF "leaked something at x.txt:3" <<<"$(tail -n 2 "$WORK/out.log")" || fail "the step's first output line is missing from the last lines"

CASE="a precheck that is killed while a step runs names that step in its last line"
new_repo
commit_as_base
add_plain_file
cat >"$WORK/repo/scripts/ci/secret-scan.sh" <<'STUB'
#!/usr/bin/env bash
pid=$$
top=
fixture=$(git rev-parse --show-toplevel)
while [ "$pid" -gt 1 ]; do
    pid=$(ps -o ppid= -p "$pid" | tr -d ' ')
    cmdline=$(tr '\0' ' ' <"/proc/$pid/cmdline" 2>/dev/null)
    if grep -q 'scripts/precheck.sh' <<<"$cmdline" && [ "$(readlink "/proc/$pid/cwd")" = "$fixture" ]; then
        top=$pid
    fi
done
kill -TERM "$top"
sleep 5
STUB
run_with_tools --lint
[ "$RC" != 0 ] || fail "a killed precheck must exit non-zero"
[ "$(tail -n 1 "$WORK/out.log")" = "FAIL precheck itself (exit $RC): died during secret scan (pending changes)" ] \
    || fail "the last line does not name the step it died in: $(tail -n 12 "$WORK/out.log" | tr '\n' '|')"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all precheck checks passed\n'
