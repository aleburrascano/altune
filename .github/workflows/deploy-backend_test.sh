#!/usr/bin/env bash

# Self-test for deploy-backend.yml, in the same shape as the deploy/*_test.sh
# scripts. Two groups of checks, both reading the workflow itself so what is
# asserted is what Actions runs:
#
#   1. The `changes` docs-only filter, LIFTED OUT OF THE YAML rather than retyped
#      and driven by fixture commits in a scratch repo. Two directions matter and
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

FILTER="$WORK/filter.sh"
awk '
    /^      - id: filter$/ { in_step = 1 }
    in_step && /^        run: \|$/ { in_run = 1; next }
    in_run && /^          / { sub(/^          /, ""); print; next }
    in_run && NF { exit }
' "$HERE/deploy-backend.yml" >"$FILTER"

if ! grep -q 'deploy=true' "$FILTER"; then
    printf 'FAIL: could not lift the filter step out of deploy-backend.yml\n'
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
    /blue-green\.sh/ { print job "\tdeploys-prod\tyes" }
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
    fail "expected one job running blue-green.sh, found: ${PROD_JOB:-none}"
    PROD_JOB=""
fi

CASE="the prod deploy is serialized and never cancelled by a newer push"
if [ -n "$PROD_JOB" ] && [ "${CANCEL[$PROD_JOB]:-<none>}" != false ]; then
    fail "job '$PROD_JOB' runs blue-green.sh under cancel-in-progress:${CANCEL[$PROD_JOB]:-<none>}; a newer push could cancel it mid-flip (#1555)"
fi

CASE="the prod deploy runs only behind the production approval gate"
if [ -n "$PROD_JOB" ] && [ -z "$(approval_gate_for "$PROD_JOB")" ]; then
    fail "job '$PROD_JOB' runs blue-green.sh without needing a job on the production environment"
fi

CASE="the workflow takes no workflow-level concurrency group"
grep -q '^concurrency:' "$HERE/deploy-backend.yml" &&
    fail "a workflow-level group cancels or queues the whole run, deploy-prod included (#1555)"

CASE="every VM checkout resets to the run's commit, never to a moving branch ref"
# #2925: `git reset --hard origin/main` lets a merge that lands while approve-prod
# waits on a human get built and shipped for an approval that named a different
# SHA. Every reset must target github.sha so staging, smoke and prod all run the
# exact commit the workflow built and had approved.
RESET_LINES=$(grep -n 'git reset --hard' "$HERE/deploy-backend.yml")
if [ -z "$RESET_LINES" ]; then
    fail "found no 'git reset --hard' in deploy-backend.yml; expected one per deploy step"
fi
BAD_RESETS=$(printf '%s\n' "$RESET_LINES" | grep -v 'github\.sha' || true)
if [ -n "$BAD_RESETS" ]; then
    fail "reset(s) not targeting \${{ github.sha }}: $BAD_RESETS"
fi

CASE="both smoke.sh invocations carry the exact commit this run is deploying"
# #2927: smoke.sh's optional third argument checks /health's version against the
# commit being shipped. Passing anything else (a moving ref, or omitting it) lets
# a stale container or failed rebuild still pass the gate.
SMOKE_LINES=$(grep -n 'bash deploy/smoke\.sh' "$HERE/deploy-backend.yml")
if [ "$(printf '%s\n' "$SMOKE_LINES" | grep -c .)" -ne 2 ]; then
    fail "expected exactly 2 smoke.sh invocations (staging + prod), found: $SMOKE_LINES"
fi
BAD_SMOKE=$(printf '%s\n' "$SMOKE_LINES" | grep -v 'github\.sha' || true)
if [ -n "$BAD_SMOKE" ]; then
    fail "smoke.sh call(s) not passing \${{ github.sha }}: $BAD_SMOKE"
fi

CASE="the prod smoke step points SMOKE_GOAPI_CONTAINER at the colour the flip just made live"
# #2992: smoke.sh's GOAPI_CONTAINER default is staging's blue container, so the
# post-swap prod smoke otherwise runs journey-check against staging, not the
# colour prod just flipped to, however the flip itself went.
PROD_SMOKE_CONTEXT=$(grep -B1 'bash deploy/smoke\.sh "https://\${{ secrets\.DEPLOY_HOST }}"' "$HERE/deploy-backend.yml")
if [ -z "$PROD_SMOKE_CONTEXT" ]; then
    fail "could not find the prod smoke.sh invocation in deploy-backend.yml"
elif ! printf '%s\n' "$PROD_SMOKE_CONTEXT" | grep -q 'SMOKE_GOAPI_CONTAINER="altune-go-api-\$(\. deploy/lib\.sh && active_color)"'; then
    fail "prod smoke step does not set SMOKE_GOAPI_CONTAINER from deploy/lib.sh's active_color: $PROD_SMOKE_CONTEXT"
fi
if printf '%s\n' "$PROD_SMOKE_CONTEXT" | grep -qi 'SMOKE_GOAPI_CONTAINER.*staging'; then
    fail "prod smoke step's SMOKE_GOAPI_CONTAINER names a staging container: $PROD_SMOKE_CONTEXT"
fi

CASE="the staging smoke step is unchanged: no SMOKE_GOAPI_CONTAINER override, staging.sh's default container still runs"
STAGING_SMOKE_CONTEXT=$(grep -B1 'bash deploy/smoke\.sh https://altune-staging\.duckdns\.org' "$HERE/deploy-backend.yml")
if [ -z "$STAGING_SMOKE_CONTEXT" ]; then
    fail "could not find the staging smoke.sh invocation in deploy-backend.yml"
elif printf '%s\n' "$STAGING_SMOKE_CONTEXT" | grep -q 'SMOKE_GOAPI_CONTAINER'; then
    fail "staging smoke step now sets SMOKE_GOAPI_CONTAINER, so it no longer exercises staging.sh's default container: $STAGING_SMOKE_CONTEXT"
fi

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all deploy path-filter and concurrency checks passed\n'
