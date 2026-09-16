#!/usr/bin/env bash

# Deploy the Overseer container to the OCI prod VM.
#
# Overseer is a single in-memory container with no go-api dependency, so it does
# not need go-api's blue-green swap (blue-green.sh only moves the go-api-* Caddy
# upstream). We rebuild the image and recreate the one container: a brief
# /overseer restart blip is acceptable and go-api traffic is untouched.
#
# The env check runs BEFORE the container is touched: a missing required
# OVERSEER_* var fails the deploy loudly here, instead of letting the new binary
# crash-loop in prod (config.validate() fails closed on the same three vars).

cd "$(dirname "$0")/.." || exit
. deploy/lib.sh

ENV_FILE="${OVERSEER_ENV_FILE:-.env.production}"
REQUIRED_VARS="OVERSEER_OWNER_USER_ID OVERSEER_SUPABASE_URL OVERSEER_SUPABASE_ANON_KEY"

OVERSEER_CONTAINER=altune-overseer
OVERSEER_DATA_DIR=/var/lib/overseer
# Seconds to watch after start-up: long enough for the first collect cycle (and any
# token rotation it triggers) to land in the logs. Overridable so the self-test can
# skip the wait.
SMOKE_WINDOW="${OVERSEER_SMOKE_WINDOW:-22}"
# TOKEN_FAILURE_SIGNATURES is defined once in deploy/lib.sh (sourced above) and
# shared with smoke.sh.

require_overseer_env() {
    if [ ! -f "$ENV_FILE" ]; then
        log "FAILED: $ENV_FILE not found; cannot deploy overseer without its env"
        exit 1
    fi
    local missing="" var
    for var in $REQUIRED_VARS; do
        # Present == a line "VAR=" with at least one non-space char of value.
        if ! grep -Eq "^[[:space:]]*${var}=.*[^[:space:]]" "$ENV_FILE"; then
            missing="$missing $var"
        fi
    done
    if [ -n "$missing" ]; then
        log "FAILED: $ENV_FILE is missing required overseer var(s):$missing"
        log "set them in $ENV_FILE before redeploying; overseer crash-loops without them"
        exit 1
    fi
}

# A fresh named volume inherits uid 1000 from the image dir (see overseer's
# Dockerfile), but a volume created on an already-deployed VM before that fix is
# still root:root, so the overseer user cannot write the token file (#1471). This
# predicate lets the deploy detect and repair that in place.
overseer_data_owned_by_app() {
    local owner
    owner=$(docker exec "$OVERSEER_CONTAINER" stat -c '%u' "$OVERSEER_DATA_DIR" 2>/dev/null || true)
    [ "$owner" = 1000 ]
}

overseer_health() {
    docker inspect -f '{{.State.Health.Status}}' "$OVERSEER_CONTAINER" 2>/dev/null || true
}

recent_token_failures() {
    docker logs --since "${SMOKE_WINDOW}s" "$OVERSEER_CONTAINER" 2>&1 \
        | grep -E "$TOKEN_FAILURE_SIGNATURES" || true
}

# Post-deploy self-verification: after the first collect cycle the overseer must be
# healthy and its logs free of operator-token persistence/seed failures (#1471).
# Unrelated collect.failed noise (e.g. OCI-usage 404, #1487) is not checked here.
smoke_check_overseer() {
    log "smoke-checking overseer for ${SMOKE_WINDOW}s (token persistence + first collect)"
    sleep "$SMOKE_WINDOW"

    local health
    health=$(overseer_health)
    if [ "$health" != healthy ]; then
        log "FAILED: overseer not healthy after ${SMOKE_WINDOW}s (health=${health:-unknown})"
        compose logs --tail 80 overseer || true
        exit 1
    fi

    local failures
    failures=$(recent_token_failures)
    if [ -n "$failures" ]; then
        log "FAILED: overseer logs show operator-token persistence/seed failure:"
        printf '%s\n' "$failures" >&2
        exit 1
    fi

    log "smoke check passed: overseer healthy, no token/persist failures"
}

require_overseer_env

log "building overseer"
compose build overseer

log "starting overseer"
compose up -d overseer

if ! overseer_data_owned_by_app; then
    log "fixing $OVERSEER_DATA_DIR ownership for uid 1000 and restarting overseer"
    docker exec -u 0 "$OVERSEER_CONTAINER" chown overseer:overseer "$OVERSEER_DATA_DIR"
    compose up -d --force-recreate overseer
fi

smoke_check_overseer

log "deployed overseer"
compose ps overseer
