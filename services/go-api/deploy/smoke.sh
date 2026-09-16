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
# A generic overseer.collect.failed (e.g. the OCI-usage 404, #1487) is tolerated,
# so only token/persist breakage fails the gate.

set -euo pipefail

cd "$(dirname "$0")/.." || exit
# TOKEN_FAILURE_SIGNATURES lives in lib.sh, shared with overseer.sh (#1471).
. deploy/lib.sh

BASE_URL=${1:?usage: smoke.sh <base-url> <overseer-container>}
OVERSEER_CONTAINER=${2:?usage: smoke.sh <base-url> <overseer-container>}
LOG_WINDOW="${SMOKE_LOG_WINDOW:-30}"

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
    docker logs --since "${LOG_WINDOW}s" "$OVERSEER_CONTAINER" 2>&1 \
        | grep -E "$TOKEN_FAILURE_SIGNATURES" || true
}

expect_status "$BASE_URL/health" 200
expect_status "$BASE_URL/overseer/" 200

log "scanning last ${LOG_WINDOW}s of $OVERSEER_CONTAINER logs for token/persist failures"
failures=$(overseer_token_failures)
if [ -n "$failures" ]; then
    log "FAILED: overseer logs show operator-token persistence/seed failure:"
    printf '%s\n' "$failures" >&2
    exit 1
fi

log "smoke gate passed: $BASE_URL healthy, overseer reachable, no token/persist failures"
