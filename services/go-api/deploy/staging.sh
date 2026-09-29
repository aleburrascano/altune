#!/usr/bin/env bash


cd "$(dirname "$0")/.." || exit
. deploy/lib.sh

use_compose_file deploy/compose.staging.yml

ENV_FILE="${STAGING_ENV_FILE:-.env.staging}"
REQUIRED_VARS="DATABASE_URL OVERSEER_SUPABASE_URL OVERSEER_SUPABASE_ANON_KEY OVERSEER_OWNER_USER_ID"
MIGRATIONS_DIR=migrations
HEALTH_TIMEOUT="${STAGING_HEALTH_TIMEOUT:-180}"
DEPLOYED_BUILD_IMAGE=altune-staging-go-api:blue
PREVIOUS_BUILD_IMAGE=altune-staging-go-api:green
PREVIOUS_BUILD_CONTAINER=altune-staging-go-api-green
DEPLOYED_BUILD_CONTAINER=altune-staging-go-api-blue

require_staging_env() {
    if [ ! -f "$ENV_FILE" ]; then
        log "FAILED: $ENV_FILE not found; cannot deploy staging without its env"
        exit 1
    fi
    local missing="" var
    for var in $REQUIRED_VARS; do
        if ! grep -Eq "^[[:space:]]*${var}=.*[^[:space:]]" "$ENV_FILE"; then
            missing="$missing $var"
        fi
    done
    if [ -n "$missing" ]; then
        log "FAILED: $ENV_FILE is missing required staging var(s):$missing"
        exit 1
    fi
}

wait_staging_healthy() {
    local deadline=$((SECONDS + HEALTH_TIMEOUT))
    while [ "$SECONDS" -lt "$deadline" ]; do
        if curl -fsS -o /dev/null --max-time 10 "$PUBLIC_HEALTH_URL"; then
            return 0
        fi
        sleep 3
    done
    return 1
}

image_exists() {
    docker image inspect "$1" >/dev/null 2>&1
}

running_build_healthy() {
    docker exec "$DEPLOYED_BUILD_CONTAINER" wget -q -O /dev/null http://127.0.0.1:8000/health 2>/dev/null
}

keep_running_build_as_previous_on_first_drill() {
    if image_exists "$PREVIOUS_BUILD_IMAGE" || ! image_exists "$DEPLOYED_BUILD_IMAGE"; then
        return 0
    fi
    if ! running_build_healthy; then
        log "rollback drill: running build is not healthy, not keeping it as the previous build"
        return 0
    fi
    if ! docker tag "$DEPLOYED_BUILD_IMAGE" "$PREVIOUS_BUILD_IMAGE"; then
        log "FAILED: rollback drill: could not keep the running build ($DEPLOYED_BUILD_IMAGE) as the previous build; nothing was migrated or rebuilt"
        exit 1
    fi
}

wait_previous_build_healthy() {
    local deadline=$((SECONDS + HEALTH_TIMEOUT))
    while [ "$SECONDS" -lt "$deadline" ]; do
        if docker exec "$PREVIOUS_BUILD_CONTAINER" wget -q -O /dev/null http://127.0.0.1:8000/health 2>/dev/null; then
            return 0
        fi
        sleep 3
    done
    return 1
}

kill_leftover_previous_build_so_it_boots_on_the_new_schema() {
    if ! compose kill go-api-green; then
        log "FAILED: rollback drill: could not clear a leftover go-api-green before booting the previous build"
        exit 1
    fi
}

boot_previous_build() {
    compose up -d --no-deps --no-build go-api-green && wait_previous_build_healthy
}

stop_previous_build_on_abort() {
    local rc=$1
    trap - EXIT INT
    log "FAILED: rollback drill interrupted; stopping go-api-green"
    compose stop go-api-green ||
        log "FAILED: rollback drill: go-api-green did not stop and may still run beside go-api-blue"
    exit "$rc"
}

run_rollback_drill() {
    if ! image_exists "$PREVIOUS_BUILD_IMAGE"; then
        log "rollback drill skipped: no previous build"
        return 0
    fi
    kill_leftover_previous_build_so_it_boots_on_the_new_schema
    log "rollback drill: booting the previous build ($PREVIOUS_BUILD_IMAGE) on the new schema"
    trap 'stop_previous_build_on_abort $?' EXIT
    trap 'stop_previous_build_on_abort 130' INT
    local drill_passed=yes
    if boot_previous_build; then
        log "rollback drill: previous build healthy on the new schema"
    else
        drill_passed=no
        log "FAILED: rollback drill: previous build unhealthy on the new schema"
        compose logs --tail 80 go-api-green || true
        log "rollback drill: a prod rollback would land on this schema; to accept an intended break, run on the VM: docker tag $DEPLOYED_BUILD_IMAGE $PREVIOUS_BUILD_IMAGE"
    fi
    if ! compose stop go-api-green; then
        drill_passed=no
        log "FAILED: rollback drill: go-api-green did not stop and may still run beside go-api-blue"
    fi
    trap - EXIT INT
    if [ "$drill_passed" = no ]; then
        exit 1
    fi
}

remember_deployed_build_as_previous() {
    if ! docker tag "$DEPLOYED_BUILD_IMAGE" "$PREVIOUS_BUILD_IMAGE"; then
        log "FAILED: staging deployed and the rollback drill passed, but $DEPLOYED_BUILD_IMAGE could not be kept as the previous build for the next drill; run on the VM: docker tag $DEPLOYED_BUILD_IMAGE $PREVIOUS_BUILD_IMAGE"
        exit 1
    fi
}

require_staging_env
resolve_public_health_url "$ENV_FILE"
keep_running_build_as_previous_on_first_drill

log "applying staging migrations"
MIGRATE_DATABASE_URL=$(read_env_var DATABASE_URL)
apply_migrations staging

log "building and recreating the staging stack"
compose up -d --build go-api-blue overseer redis

if ! wait_staging_healthy; then
    log "FAILED: $PUBLIC_HEALTH_URL not healthy after ${HEALTH_TIMEOUT}s"
    compose logs --tail 80 go-api-blue || true
    exit 1
fi

run_rollback_drill
remember_deployed_build_as_previous

log "deployed staging"
compose ps
