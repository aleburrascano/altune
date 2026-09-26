#!/usr/bin/env bash

# Smoke gate for a deployed tier (epic #1488, task #1492). Tier-agnostic so prod
# can reuse it after promotion:
#   bash deploy/smoke.sh https://altune-staging.duckdns.org altune-staging-overseer
#   bash deploy/smoke.sh https://altune.duckdns.org         altune-overseer
#
# Exits non-zero if the tier is unhealthy, so the pipeline blocks promotion. Runs
# on the VM (needs `docker logs` for the overseer container). Checks:
#   1. go-api /health == 200
#   2. /overseer/ reachable (SPA served)
#   3. no operator-token persistence/seed failure in recent overseer logs (#1471)
#   4. overseer /health == 200 with buckets_ok>=1, proving the collect loop is
#      live AND at least one bucket collected (#1812 moved loop liveness off the
#      per-tick log heartbeat and onto this endpoint; the heartbeat is now DEBUG
#      and absent at staging/prod log levels, #1820).
#   5. `/app journey-check` inside the go-api container ($SMOKE_GOAPI_CONTAINER,
#      default altune-staging-go-api-blue): one real discovery search and one
#      real yt-dlp download of the YouTube canary with the app's own format
#      selector, egress IP and cookie jar (#2928). Liveness alone stayed green
#      through five weeks of dead YouTube downloads (#2788).
# A generic overseer.collect.failed (e.g. the OCI-usage 404, #1487) is tolerated:
# a partial-failure cycle still reports buckets_ok>=1. Only token/persist breakage,
# a stalled/dead loop (/health non-200), or an all-sources-down cycle
# (buckets_ok=0) fails the gate.

set -euo pipefail

cd "$(dirname "$0")/.." || exit
# TOKEN_FAILURE_SIGNATURES lives in lib.sh, shared with overseer.sh (#1471).
. deploy/lib.sh

BASE_URL=${1:?usage: smoke.sh <base-url> <overseer-container>}
OVERSEER_CONTAINER=${2:?usage: smoke.sh <base-url> <overseer-container>}
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
    printf '%s\n' "$1" | grep -E "$TOKEN_FAILURE_SIGNATURES" || true
}

# overseer_health prints the /health JSON body to stdout and exits non-zero unless
# the endpoint answered 200. A stalled or dead collect loop answers 503 (#1812), the
# liveness failure the gate must block on — it replaces the old log-heartbeat scan
# now that the heartbeat is DEBUG and absent at staging/prod log levels (#1820).
overseer_health() {
    local response status
    response=$(curl -s -w '\n%{http_code}' --max-time 15 --retry 5 --retry-delay 3 "$1")
    status=$(printf '%s' "$response" | tail -n1)
    printf '%s' "$response" | sed '$d'
    [ "$status" = "200" ]
}

# overseer_buckets_ok extracts buckets_ok from the /health JSON body, defaulting to
# 0 when the field is absent so an unexpected body reads as an all-down cycle.
overseer_buckets_ok() {
    printf '%s' "$1" | grep -oE '"buckets_ok" *: *[0-9]+' | grep -oE '[0-9]+' || true
}

expect_status "$BASE_URL/health" 200
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
if ! journey=$(docker exec "$GOAPI_CONTAINER" /app journey-check 2>&1); then
    log "FAILED: journey-check in $GOAPI_CONTAINER exited non-zero (search or download broken):"
    printf '%s\n' "$journey" | tail -n 20 >&2
    exit 1
fi
printf '%s\n' "$journey" | grep 'journey-check:' >&2 || true

log "smoke gate passed: $BASE_URL healthy, overseer reachable, ${ok_count} bucket(s) collected, no token/persist failures, search and download work"
