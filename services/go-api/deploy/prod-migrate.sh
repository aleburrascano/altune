#!/usr/bin/env bash

cd "$(dirname "$0")/.." || exit
. deploy/lib.sh

ENV_FILE="${PROD_ENV_FILE:-.env.production}"
MIGRATIONS_DIR=migrations
MIGRATE_TIER=prod

if [ ! -f "$ENV_FILE" ]; then
    log "FAILED: $ENV_FILE not found; cannot apply prod migrations"
    exit 1
fi

MIGRATE_DATABASE_URL=$(read_env_var DATABASE_URL)
if [ -z "$MIGRATE_DATABASE_URL" ]; then
    log "FAILED: DATABASE_URL is unset or empty in $ENV_FILE"
    exit 1
fi

require_psql
ensure_migration_tracker

if [ "$(tracked_migration_count)" = 0 ] && schema_is_present; then
    log "FAILED: prod migration baseline is not established: the schema exists but schema_migrations is empty; record prod's applied migrations in it by hand"
    exit 1
fi

log "applying prod migrations"
apply_new_migrations
log "prod migrations up to date"
