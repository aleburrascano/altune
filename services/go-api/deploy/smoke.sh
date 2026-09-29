#!/usr/bin/env bash


set -euo pipefail

cd "$(dirname "$0")/.." || exit
. deploy/lib.sh

BASE_URL=${1:?usage: smoke.sh <base-url> <overseer-container> [expected-commit]}
OVERSEER_CONTAINER=${2:?usage: smoke.sh <base-url> <overseer-container> [expected-commit]}
EXPECTED_COMMIT="${3:-}"
LOG_WINDOW="${SMOKE_LOG_WINDOW:-30}"
GOAPI_CONTAINER="${SMOKE_GOAPI_CONTAINER:-altune-staging-go-api-blue}"

log() {
    printf '[smoke] %s\n' "$*" >&2
}

http_status() {
    curl -s -o /dev/null -w '%{http_code}' --max-time 15 --retry 5 --retry-delay 3 "$1"
}

expect_status() {
    local url=$1 want=$2 got
    got=$(http_status "$url")
    if [ "$got" != "$want" ]; then
        log "FAILED: $url returned $got, expected $want"
        exit 1
    fi
    log "ok: $url -> $got"
}

overseer_token_failures() {
    printf '%s\n' "$1" | token_failures
}

overseer_health() {
    local response status
    response=$(curl -s -w '\n%{http_code}' --max-time 15 --retry 5 --retry-delay 3 "$1")
    status=$(printf '%s' "$response" | tail -n1)
    printf '%s' "$response" | sed '$d'
    [ "$status" = "200" ]
}

overseer_buckets_ok() {
    printf '%s' "$1" | grep -oE '"buckets_ok" *: *[0-9]+' | grep -oE '[0-9]+' || true
}

goapi_version() {
    printf '%s' "$1" | grep -oE '"version" *: *"[^"]*"' | grep -oE '"[^"]*"$' | tr -d '"' || true
}

expect_status "$BASE_URL/health" 200

if [ -n "$EXPECTED_COMMIT" ]; then
    health_body=$(curl -s --max-time 15 --retry 5 --retry-delay 3 "$BASE_URL/health")
    got_version=$(goapi_version "$health_body")
    if [ "$got_version" != "$EXPECTED_COMMIT" ]; then
        log "FAILED: running version ${got_version:-<missing>}, expected $EXPECTED_COMMIT"
        exit 1
    fi
    log "ok: running version $got_version"
fi

expect_status "$BASE_URL/overseer/" 200

log "scanning last ${LOG_WINDOW}s of $OVERSEER_CONTAINER logs"
logs=$(docker logs --since "${LOG_WINDOW}s" "$OVERSEER_CONTAINER" 2>&1)

failures=$(overseer_token_failures "$logs")
if [ -n "$failures" ]; then
    log "FAILED: overseer logs show operator-token persistence/seed failure:"
    printf '%s\n' "$failures" >&2
    exit 1
fi

if ! health=$(overseer_health "$BASE_URL/overseer/health"); then
    log "FAILED: overseer /health did not return 200 (collect loop stalled or dead)"
    exit 1
fi
ok_count=$(overseer_buckets_ok "$health")
if [ "${ok_count:-0}" -lt 1 ]; then
    log "FAILED: overseer /health reports buckets_ok=${ok_count:-0} (<1)"
    log "(an all-sources-down cycle: no successful collection observed)"
    exit 1
fi

log "running journey-check in $GOAPI_CONTAINER"
JOURNEY_TIMEOUT="${SMOKE_JOURNEY_TIMEOUT:-6m}"
journey=$(timeout "$JOURNEY_TIMEOUT" docker exec "$GOAPI_CONTAINER" /app journey-check 2>&1) && rc=0 || rc=$?
if [ "$rc" != 0 ]; then
    if [ "$rc" = 124 ]; then
        log "FAILED: journey-check in $GOAPI_CONTAINER timed out after $JOURNEY_TIMEOUT:"
    else
        log "FAILED: journey-check in $GOAPI_CONTAINER exited non-zero (search or download broken):"
    fi
    printf '%s\n' "$journey" | tail -n 20 >&2
    exit 1
fi
printf '%s\n' "$journey" | grep 'journey-check:' >&2 || true

log "smoke gate passed: $BASE_URL healthy, overseer reachable, ${ok_count} bucket(s) collected, no token/persist failures, search and download work"
