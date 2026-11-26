#!/usr/bin/env bash

cd "$(dirname "$0")/.." || exit
. deploy/prod_host.sh

MIGRATE_ENV_FILE="${PROD_MIGRATE_ENV_FILE:-.env.migrate.production}"
MIGRATIONS_DIR=migrations
MIGRATE_TIER=prod

MIGRATE_DATABASE_URL=$(migrate_database_url "$MIGRATE_ENV_FILE") || exit 1

require_psql
ensure_migration_tracker

if [ "$(tracked_migration_count)" = 0 ] && schema_is_present; then
    log "FAILED: prod migration baseline is not established: the schema exists but schema_migrations is empty; record prod's applied migrations in it by hand"
    exit 1
fi

log "applying prod migrations"
apply_new_migrations
log "prod migrations up to date"
