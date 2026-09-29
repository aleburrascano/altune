#!/usr/bin/env bash


cd "$(dirname "$0")/.." || exit
. deploy/lib.sh

ENV_FILE="${OVERSEER_ENV_FILE:-.env.production}"
REQUIRED_VARS="OVERSEER_OWNER_USER_ID OVERSEER_SUPABASE_URL OVERSEER_SUPABASE_ANON_KEY"

OVERSEER_CONTAINER=altune-overseer
OVERSEER_DATA_DIR=/var/lib/overseer
SMOKE_WINDOW="${OVERSEER_SMOKE_WINDOW:-22}"

require_overseer_env() {
    if [ ! -f "$ENV_FILE" ]; then
        log "FAILED: $ENV_FILE not found; cannot deploy overseer without its env"
        exit 1
    fi
    local missing="" var
    for var in $REQUIRED_VARS; do
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

overseer_data_owned_by_app() {
    local owner
    owner=$(docker exec "$OVERSEER_CONTAINER" stat -c '%u' "$OVERSEER_DATA_DIR" 2>/dev/null || true)
    [ "$owner" = 1000 ]
}

overseer_health() {
    docker inspect -f '{{.State.Health.Status}}' "$OVERSEER_CONTAINER" 2>/dev/null || true
}

recent_token_failures() {
    docker logs --since "${SMOKE_WINDOW}s" "$OVERSEER_CONTAINER" 2>&1 | token_failures
}

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
