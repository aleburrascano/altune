#!/usr/bin/env bash

# Deploy the STAGING backend stack to the OCI VM (epic #1488, tasks #1491/#1492).
#
# Staging runs on the SAME VM as prod but as a separate Compose project
# (compose.staging.yml, project `staging`, containers altune-staging-*). This
# script ONLY builds/recreates those staging containers and applies migrations to
# the STAGING Supabase project. It never touches prod: prod's blue-green.sh +
# overseer.sh run ONLY in the workflow's deploy-prod job, behind the `production`
# environment's manual-approval gate.
#
# Unlike prod, staging AUTO-APPLIES migrations (prod migrations stay manual, see
# deploy-backend.yml's warn step). The two Supabase projects must stay in lockstep;
# staging is where a migration is proven before prod. Migrations are tracked in a
# schema_migrations table so a non-idempotent migration (e.g. 016's bare
# ADD CONSTRAINT) is applied exactly once. An already-migrated DB with no tracking
# table is adopted at the current baseline rather than re-run.
#
# Fails fast (set -euo pipefail via lib.sh) on a missing staging env var, a
# migration error, or an unhealthy go-api, so a broken staging deploy blocks the
# smoke gate that gates promotion.

cd "$(dirname "$0")/.." || exit
. deploy/lib.sh

# Redirect lib.sh's compose() and verify_public() at the staging tier. compose()
# reads $COMPOSE_FILE at call time, so this points every `compose` call here at the
# staging project; nothing in this script calls the prod-only upstream/flip helpers.
# shellcheck disable=SC2034  # consumed by lib.sh's compose() after this source
COMPOSE_FILE=deploy/compose.staging.yml
PUBLIC_HEALTH_URL="${STAGING_HEALTH_URL:-https://altune-staging.duckdns.org/health}"

ENV_FILE="${STAGING_ENV_FILE:-.env.staging}"
REQUIRED_VARS="DATABASE_URL OVERSEER_SUPABASE_URL OVERSEER_SUPABASE_ANON_KEY OVERSEER_OWNER_USER_ID"
MIGRATIONS_DIR=migrations
HEALTH_TIMEOUT="${STAGING_HEALTH_TIMEOUT:-180}"

require_staging_env() {
    if [ ! -f "$ENV_FILE" ]; then
        log "FAILED: $ENV_FILE not found; cannot deploy staging without its env"
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
        log "FAILED: $ENV_FILE is missing required staging var(s):$missing"
        exit 1
    fi
}

read_env_var() {
    grep -E "^[[:space:]]*$1=" "$ENV_FILE" | head -1 | sed -E "s/^[[:space:]]*$1=//"
}

migration_versions() {
    local file
    for file in "$MIGRATIONS_DIR"/*.sql; do
        basename "$file" .sql
    done | sort -V
}

psql_value() {
    psql "$STAGING_DATABASE_URL" -tA -v ON_ERROR_STOP=1 -c "$1"
}

# An already-migrated DB predates this tracker (migrations were applied by hand):
# the baseline table exists but nothing is tracked. Adopt the current set as the
# baseline so already-applied, non-idempotent migrations are never re-run. Assumes
# the two projects are in lockstep (the documented staging invariant).
adopt_existing_schema() {
    [ "$(psql_value 'SELECT count(*) FROM schema_migrations;')" = 0 ] || return 0
    [ "$(psql_value "SELECT to_regclass('public.tracks') IS NOT NULL;")" = t ] || return 0
    log "adopting existing staging schema as the migration baseline"
    local version
    for version in $(migration_versions); do
        psql_value "INSERT INTO schema_migrations (version) VALUES ('$version') ON CONFLICT DO NOTHING;" >/dev/null
    done
}

apply_staging_migrations() {
    command -v psql >/dev/null || { log "FAILED: psql not found on PATH"; exit 1; }
    STAGING_DATABASE_URL=$(read_env_var DATABASE_URL)
    psql_value 'CREATE TABLE IF NOT EXISTS schema_migrations (
        version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now());' >/dev/null
    adopt_existing_schema
    local version
    for version in $(migration_versions); do
        if [ "$(psql_value "SELECT 1 FROM schema_migrations WHERE version='$version';")" = 1 ]; then
            continue
        fi
        log "applying migration $version"
        psql "$STAGING_DATABASE_URL" -v ON_ERROR_STOP=1 --single-transaction \
            -f "$MIGRATIONS_DIR/$version.sql" \
            -c "INSERT INTO schema_migrations (version) VALUES ('$version');"
    done
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

require_staging_env

log "applying staging migrations"
apply_staging_migrations

# A brief staging blip is acceptable (design Decision 1): recreate go-api-blue,
# overseer, and redis in place rather than run a full blue-green flip. Caddy
# (prod's, shared) already imports staging-upstream.conf -> altune-staging-go-api-blue.
log "building and recreating the staging stack"
compose up -d --build go-api-blue overseer redis

if ! wait_staging_healthy; then
    log "FAILED: $PUBLIC_HEALTH_URL not healthy after ${HEALTH_TIMEOUT}s"
    compose logs --tail 80 go-api-blue || true
    exit 1
fi

log "deployed staging"
compose ps
