#!/usr/bin/env bash

# Self-test for deploy-backend.yml, in the same shape as the deploy/*_test.sh
# scripts. Two groups of checks, both reading the workflow itself so what is
# asserted is what Actions runs:
#
#   1. The `changes` docs-only filter (deploy-backend-changes.sh, which the
#      workflow's filter step runs), driven by fixture commits in a scratch repo. Two directions matter and
#      only one is loud: a false deploy=true costs one needless prod-gate
#      approval, while a false deploy=false silently swallows a real prod deploy
#      (#1553 — why the exclusion is *.md and never a directory name like
#      */docs/*).
#   2. The concurrency invariants that keep prod promotion reachable (#1807) and
#      uninterruptible (#1555). These are structural, so the job graph is read out
#      of the YAML and asserted directly — the deadlock they guard can otherwise
#      only be observed by wedging a real prod deploy.
#
# GitHub only parses *.yml in this directory, so this file sits inert beside the
# workflow. Run it by hand:
#   bash .github/workflows/deploy-backend_test.sh

set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
FAILURES=0
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

FILTER="$HERE/deploy-backend-changes.sh"

if ! awk '
    /^      - id: filter$/ { in_step = 1; next }
    in_step && /^      - / { exit 1 }
    in_step && /^        run: bash \.github\/workflows\/deploy-backend-changes\.sh$/ { found = 1; exit }
    END { exit !found }
' "$HERE/deploy-backend.yml"; then
    printf 'FAIL: the filter step of deploy-backend.yml does not run deploy-backend-changes.sh\n'
    exit 1
fi

git init -q -b main "$WORK/repo"
git -C "$WORK/repo" config user.email test@altune.local
git -C "$WORK/repo" config user.name "filter test"
mkdir -p "$WORK/repo/services/go-api"
echo base >"$WORK/repo/services/go-api/main.go"
git -C "$WORK/repo" add -A
git -C "$WORK/repo" commit -qm base
BASE=$(git -C "$WORK/repo" rev-parse HEAD)

fail() {
    printf 'FAIL: %s\n      %s\n' "$CASE" "$1"
    FAILURES=$((FAILURES + 1))
}

# expect_deploy <true|false> <changed path>...
expect_deploy() {
    local want=$1 f
    shift
    git -C "$WORK/repo" reset -q --hard "$BASE"
    for f in "$@"; do
        mkdir -p "$WORK/repo/$(dirname "$f")"
        echo change >>"$WORK/repo/$f"
    done
    git -C "$WORK/repo" add -A
    git -C "$WORK/repo" commit -qm "$CASE"

    local got
    got=$(run_filter push "$BASE" "$(git -C "$WORK/repo" rev-parse HEAD)")
    [ "$got" = "$want" ] || fail "expected deploy=$want, got deploy=$got"
}

# run_filter <event> <before> <after> -> the deploy= value the step would output.
run_filter() {
    local out="$WORK/output"
    : >"$out"
    (cd "$WORK/repo" && EVENT=$1 BEFORE=$2 AFTER=$3 GITHUB_OUTPUT="$out" \
        bash "$FILTER" >"$WORK/out.log" 2>&1)
    sed -n 's/^deploy=//p' "$out"
}

CASE="a deployable .go file under a docs/ dir still deploys"
expect_deploy true services/go-api/internal/docs/registry.go

CASE="a non-.md asset under a docs/ dir still deploys"
expect_deploy true services/overseer/docs/dashboard.tmpl

CASE="a docs-only push (*.md) skips the deploy chain"
expect_deploy false services/go-api/RUNBOOK.md docs/features/deploy/design.md

CASE="an .md inside a services docs/ dir still skips"
expect_deploy false services/go-api/internal/docs/guide.md

CASE="ordinary backend code deploys"
expect_deploy true services/go-api/internal/app/service.go

CASE="a doc alongside real code deploys"
expect_deploy true services/go-api/RUNBOOK.md services/go-api/main.go

CASE="a push touching nothing in the backend skips"
expect_deploy false apps/mobile/App.tsx

CASE="a manual dispatch has no diff base and fails safe"
got=$(run_filter workflow_dispatch "" HEAD)
[ "$got" = true ] || fail "expected deploy=true, got deploy=$got"

CASE="a first push of a branch (null before) fails safe"
got=$(run_filter push 0000000000000000000000000000000000000000 HEAD)
[ "$got" = true ] || fail "expected deploy=true, got deploy=$got"

CASE="a force-pushed-away diff base fails safe"
got=$(run_filter push 1111111111111111111111111111111111111111 HEAD)
[ "$got" = true ] || fail "expected deploy=true, got deploy=$got"

# --- concurrency invariants -------------------------------------------------
# One fact per line, `job<TAB>key<TAB>value`, for every job in the workflow.
# Comments are skipped: a comment block sits above the job it documents but below
# the previous job's last line, so keeping them would attribute its text to the
# wrong job.
FACTS="$WORK/jobs.tsv"
awk '
    function value(  v) { v = $0; sub(/^ *[a-z-]+: */, "", v); return v }
    /^ *#/ { next }
    /^jobs:$/ { in_jobs = 1; next }
    !in_jobs { next }
    /^  [a-z][a-z0-9-]*:$/ { job = $1; sub(/:$/, "", job); in_group = 0; next }
    job == "" { next }
    /^    concurrency:$/ { in_group = 1; next }
    /^    environment: / { print job "\tenvironment\t" value(); next }
    /^    needs: / { needs = value(); gsub(/[][,]/, " ", needs); print job "\tneeds\t" needs; next }
    in_group && /^      group: / { print job "\tgroup\t" value(); next }
    in_group && /^      cancel-in-progress: / { print job "\tcancel\t" value(); next }
    /^    [a-z]/ { in_group = 0 }
    /release-prod prod / { print job "\tdeploys-prod\tyes" }
' "$HERE/deploy-backend.yml" >"$FACTS"

declare -A ENVIRONMENT CANCEL NEEDS
while IFS=$'\t' read -r job key value; do
    case "$key" in
        environment) ENVIRONMENT[$job]=$value ;;
        cancel) CANCEL[$job]=$value ;;
        needs) NEEDS[$job]=$value ;;
    esac
done <"$FACTS"

# approval_gate_for <job> -> the upstream job that waits on the production
# environment, or empty. The `needs` graph is acyclic (Actions rejects a cycle),
# so the walk terminates.
approval_gate_for() {
    local job=$1 dep upstream
    for dep in ${NEEDS[$job]:-}; do
        if [ "${ENVIRONMENT[$dep]:-}" = production ]; then
            printf '%s' "$dep"
            return
        fi
        upstream=$(approval_gate_for "$dep")
        if [ -n "$upstream" ]; then
            printf '%s' "$upstream"
            return
        fi
    done
}

CASE="a job that waits for approval holds no uncancellable serialize lock"
# The #1807 deadlock: `environment:` makes a job wait on a human while it already
# occupies its concurrency group, and cancel-in-progress:false never hands that
# group to the successor — whose deployment then never becomes reviewable (the
# approval POST returns HTTP 422), so the prod gate is unreachable.
for job in "${!ENVIRONMENT[@]}"; do
    [ "${CANCEL[$job]:-}" = false ] &&
        fail "job '$job' gates on environment ${ENVIRONMENT[$job]} while holding a cancel-in-progress:false group"
done

CASE="exactly one job runs the prod deploy"
PROD_JOB=$(awk -F'\t' '$2 == "deploys-prod" { print $1 }' "$FACTS" | sort -u)
if [ "$(printf '%s' "$PROD_JOB" | grep -c .)" -ne 1 ]; then
    fail "expected one job running the prod release, found: ${PROD_JOB:-none}"
    PROD_JOB=""
fi

CASE="the prod deploy is serialized and never cancelled by a newer push"
if [ -n "$PROD_JOB" ] && [ "${CANCEL[$PROD_JOB]:-<none>}" != false ]; then
    fail "job '$PROD_JOB' runs the prod release under cancel-in-progress:${CANCEL[$PROD_JOB]:-<none>}; a newer push could cancel it mid-flip (#1555)"
fi

CASE="the prod deploy runs only behind the production approval gate"
if [ -n "$PROD_JOB" ] && [ -z "$(approval_gate_for "$PROD_JOB")" ]; then
    fail "job '$PROD_JOB' runs the prod release without needing a job on the production environment"
fi

CASE="the workflow takes no workflow-level concurrency group"
grep -q '^concurrency:' "$HERE/deploy-backend.yml" &&
    fail "a workflow-level group cancels or queues the whole run, deploy-prod included (#1555)"

CASE="every VM release runs the release script of the run's commit, at that commit"
SHA_EXPR="\${{ github.sha }}"
RELEASE_SHOWS=$(grep -cF "git -C ~/altune show $SHA_EXPR:services/go-api/deploy/release.sh" "$HERE/deploy-backend.yml")
[ "$RELEASE_SHOWS" = 2 ] || fail "expected 2 releases fetched from \${{ github.sha }}, found $RELEASE_SHOWS"
RELEASE_RUNS=$(grep -E 'bash ~/\.altune-release-(staging|prod) ' "$HERE/deploy-backend.yml")
[ "$(printf '%s\n' "$RELEASE_RUNS" | grep -c 'github\.sha')" = 2 ] ||
    fail "expected staging and prod releases both passed \${{ github.sha }}: $RELEASE_RUNS"
grep -qF "reset --hard \"\$sha\"" "$HERE/../../services/go-api/deploy/release.sh" ||
    fail "release.sh no longer resets the checkout to the sha it was given"

CASE="every multi-line SSH script stops at its first failed line"
UNGUARDED=$(awk '/^          script: \|$/ { getline; if ($0 !~ /^            set -e$/) print NR ": " $0 }' "$HERE/deploy-backend.yml")
[ -z "$UNGUARDED" ] || fail "script blocks not opening with set -e: $UNGUARDED"

CASE="no step passes the script_stop input appleboy/ssh-action@v1.2.5 rejects"
grep -q 'script_stop' "$HERE/deploy-backend.yml" &&
    fail "deploy-backend.yml still passes script_stop, which v1.2.5 no longer accepts"

CASE="no workflow anywhere passes the script_stop input appleboy/ssh-action@v1.2.5 rejects"
OFFENDERS=$(grep -l 'script_stop' "$HERE"/*.yml)
[ -z "$OFFENDERS" ] ||
    fail "still passes script_stop, which v1.2.5 no longer accepts: $OFFENDERS"

CASE="the staging smoke carries the exact commit and the staging container default"
STAGING_SMOKE=$(grep 'bash deploy/smoke\.sh' "$HERE/deploy-backend.yml")
[ "$(printf '%s\n' "$STAGING_SMOKE" | grep -c .)" = 1 ] ||
    fail "expected exactly 1 smoke.sh invocation (staging), found: $STAGING_SMOKE"
printf '%s\n' "$STAGING_SMOKE" | grep -qF "bash deploy/smoke.sh \${{ vars.STAGING_API_URL }} altune-staging-overseer $SHA_EXPR" ||
    fail "staging smoke does not pass the staging url, overseer and \${{ github.sha }}: $STAGING_SMOKE"
grep -q 'SMOKE_GOAPI_CONTAINER' "$HERE/deploy-backend.yml" &&
    fail "deploy-backend.yml sets SMOKE_GOAPI_CONTAINER, so the staging smoke no longer exercises staging.sh's default container"

CASE="the prod release smokes the public url"
grep -qF "bash ~/.altune-release-prod prod $SHA_EXPR \${{ vars.PROD_API_URL }}" "$HERE/deploy-backend.yml" ||
    fail "the prod release is not given vars.PROD_API_URL to smoke"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all deploy path-filter and concurrency checks passed\n'
