#!/usr/bin/env bash

cd "$(dirname "$0")/.."
. deploy/lib.sh

ACTIVE=$(active_color)
PREVIOUS=$(idle_color "$ACTIVE")

log "rolling back from $ACTIVE to $PREVIOUS"

compose up -d --no-deps "go-api-$PREVIOUS"

if ! wait_healthy "$PREVIOUS"; then
    log "FAILED: $PREVIOUS never became healthy; staying on $ACTIVE"
    compose logs --tail 80 "go-api-$PREVIOUS" || true
    exit 1
fi

flip_to "$PREVIOUS"

if ! verify_public; then
    log "FAILED: $PUBLIC_HEALTH_URL still failing; returning to $ACTIVE"
    flip_to "$ACTIVE"
    exit 1
fi

compose stop "go-api-$ACTIVE" || true

log "rolled back to $PREVIOUS"
compose ps
