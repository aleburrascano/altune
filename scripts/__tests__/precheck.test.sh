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
    printf '#!/usr/bin/env bash\nprintf "go %%s\\n" "$*" >>"%s/tools.log"\nexec "%s" "$@"\n' "$WORK" "$REAL_GO" >"$WORK/bin/go"
    printf '#!/usr/bin/env bash\nprintf "golangci-lint %%s\\n" "$*" >>"%s/tools.log"\n' "$WORK" >"$WORK/bin/golangci-lint"
    printf '#!/usr/bin/env bash\nprintf "govulncheck %%s\\n" "$*" >>"%s/tools.log"\n' "$WORK" >"$WORK/bin/govulncheck"
    chmod +x "$WORK/bin/go" "$WORK/bin/golangci-lint" "$WORK/bin/govulncheck"
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

CASE="a go-api change in a package the selection gate does not import skips the gate and govulncheck, and vets only that package"
new_repo
add_go_fixture
edit_go_file internal/discovery/adapters/x/x.go
run_with_tools
forbid_log "run ./cmd/acquisitioneval"
forbid_log "govulncheck"
expect_log "vet altune/go-api/internal/discovery/adapters/x"
forbid_log "vet ./..."

CASE="a go-api change in a package the selection gate imports runs the gate and vets every importer"
new_repo
add_go_fixture
edit_go_file internal/shared/s/s.go
run_with_tools
expect_log "run ./cmd/acquisitioneval"
forbid_log "govulncheck"
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

CASE="the import-direction lint reads only new lines against the base"
new_repo
add_go_fixture
edit_go_file internal/discovery/adapters/x/x.go
run_with_tools
expect_log "golangci-lint run --new-from-rev="

CASE="a change to the golangci config lints the whole tree"
new_repo
add_go_fixture
(
    cd "$WORK/repo" || exit 1
    printf 'version: "2"\n' >services/go-api/.golangci.yml
    git add -A
    git commit -qm "config"
)
run_with_tools
expect_log "golangci-lint run --allow-serial-runners"

CASE="a renamed go file lints the whole tree"
new_repo
add_go_fixture
(
    cd "$WORK/repo" || exit 1
    git mv services/go-api/internal/discovery/adapters/x/x.go services/go-api/internal/discovery/adapters/x/y.go
    git commit -qm "rename"
)
run_with_tools
expect_log "golangci-lint run --allow-serial-runners"

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

add_react_doctor_stubs() {
    (
        cd "$WORK/repo" || exit 1
        mkdir -p client/scripts scripts/ci client/src
        printf 'import { appendFileSync } from "node:fs";\nappendFileSync(process.env.TOOLS_LOG, "changed-lines " + process.argv.slice(2).join(" ") + "\\n");\n' >client/scripts/react-doctor-changed-lines.mjs
        printf 'echo whole-tree >>"$TOOLS_LOG"\n' >scripts/ci/test-mobile-react-doctor.sh
    )
    commit_as_base
}

CASE="a client-only change runs only the changed-lines react-doctor, scanning for itself"
new_repo
add_mobile_stubs
add_react_doctor_stubs
(
    cd "$WORK/repo" || exit 1
    printf 'export const x = 1;\n' >client/src/x.ts
    git add -A
    git commit -qm "client change"
)
TOOLS_LOG="$WORK/tools.log" run_script_with_stubs
expect_log "changed-lines "
forbid_log "whole-tree"
forbid_log "doctor-report.json"

add_client_base() {
    (
        cd "$WORK/repo" || exit 1
        mkdir -p client/src
        printf "import { a } from './a';\nexport const x = a;\n" >client/src/x.ts
        printf 'export const a = 1;\n' >client/src/a.ts
    )
    commit_as_base
}

edit_client_file() {
    (
        cd "$WORK/repo" || exit 1
        printf '%s\n' "$2" >"client/src/$1"
        git add -A
        git commit -qm "edit $1"
    )
}

CASE="a ts change that leaves every import line alone skips the cycle check"
new_repo
add_mobile_stubs
add_client_base
edit_client_file x.ts "import { a } from './a';
export const x = a + 1;"
run_script_with_stubs
forbid_out "no dependency cycles"

CASE="a ts change that edits an import line runs the cycle check"
new_repo
add_mobile_stubs
add_client_base
edit_client_file x.ts "import { a } from './b';
export const x = a;"
run_script_with_stubs
expect_out "no dependency cycles"

CASE="a ts change that adds a multi-line export-from runs the cycle check"
new_repo
add_mobile_stubs
add_client_base
edit_client_file x.ts "import { a } from './a';
export {
  a,
} from './c';"
run_script_with_stubs
expect_out "no dependency cycles"

CASE="a new ts file runs the cycle check"
new_repo
add_mobile_stubs
add_client_base
edit_client_file z.ts "export const z = 1;"
run_script_with_stubs
expect_out "no dependency cycles"

CASE="a deleted ts file runs the cycle check"
new_repo
add_mobile_stubs
add_client_base
(
    cd "$WORK/repo" || exit 1
    git rm -q client/src/a.ts
    git commit -qm "delete a"
)
run_script_with_stubs
expect_out "no dependency cycles"

CASE="a go change that leaves the import block alone skips the cycle check, and one that edits it runs the check"
new_repo
add_mobile_stubs
add_go_fixture
edit_go_file internal/acq/a.go
run_with_tools
forbid_out "no dependency cycles"
(
    cd "$WORK/repo" || exit 1
    git update-ref refs/remotes/gitea/main HEAD
    printf 'package acq\n\nimport (\n\t"altune/go-api/internal/shared/s"\n\t"fmt"\n)\n\nvar _ = fmt.Sprint\n\nfunc A() int { return s.S() }\n' >services/go-api/internal/acq/a.go
    git add -A
    git commit -qm "import fmt"
)
run_with_tools
expect_out "no dependency cycles"

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
expect_log "test -shuffle=on -count=1 ./internal/discovery/adapters/x"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all precheck checks passed\n'
