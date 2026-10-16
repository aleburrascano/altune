#!/usr/bin/env bash

cd "$(dirname "$0")/.." || exit
. deploy/prod_host.sh
resolve_public_health_url "${PROD_ENV_FILE:-.env.production}"

archive_colour_logs() {
    local container="altune-go-api-$1" names dir archive
    [ -n "${HOME:-}" ] || return 1
    names=$(docker ps -a --format '{{.Names}}' 2>/dev/null) || return 0
    grep -qx "$container" <<<"$names" || return 0
    dir="$HOME/altune-logs"
    archive="$dir/go-api-$1-$(date -u +%Y%m%dT%H%M%SZ).log.gz"
    mkdir -p "$dir" || return 1
    if timeout 60 docker logs "$container" 2>&1 | gzip >"$archive.partial"; then
        mv "$archive.partial" "$archive" || return 1
    else
        rm -f "$archive.partial"
        return 1
    fi
    find "$dir" -maxdepth 1 -name 'go-api-*.log.gz' -mtime +30 -delete || return 1
}

ACTIVE=$(active_color)
TARGET=$(idle_color "$ACTIVE")
capture_upstream

log "active=$ACTIVE target=$TARGET"

compose up -d redis caddy pot

log "building $TARGET"
compose build "go-api-$TARGET"

archive_colour_logs "$TARGET" || log "WARNING: could not archive the logs of go-api-$TARGET; deploying anyway"

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
