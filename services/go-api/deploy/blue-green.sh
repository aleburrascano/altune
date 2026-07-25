#!/usr/bin/env bash

cd "$(dirname "$0")/.."
. deploy/lib.sh

ACTIVE=$(active_color)
TARGET=$(idle_color "$ACTIVE")
capture_upstream

log "active=$ACTIVE target=$TARGET"

compose up -d redis caddy

log "building $TARGET"
compose build "go-api-$TARGET"

log "starting $TARGET"
compose up -d --no-deps "go-api-$TARGET"

if ! wait_healthy "$TARGET"; then
    log "FAILED: $TARGET never became healthy after ${HEALTH_TIMEOUT}s"
    compose logs --tail 80 "go-api-$TARGET" || true
    compose stop "go-api-$TARGET" || true
    log "$ACTIVE is still serving; production was never touched"
    exit 1
fi

log "flipping traffic to $TARGET"
flip_to "$TARGET"

if ! verify_public; then
    log "ROLLBACK: $PUBLIC_HEALTH_URL failed after the flip"
    restore_upstream
    compose stop "go-api-$TARGET" || true
    log "rolled back to: $PREVIOUS_UPSTREAM"
    exit 1
fi

log "draining $ACTIVE for ${DRAIN_SECONDS}s"
sleep "$DRAIN_SECONDS"
compose stop "go-api-$ACTIVE" || true

docker rm -f altune-go-api >/dev/null 2>&1 || true

log "deployed $TARGET"
compose ps
