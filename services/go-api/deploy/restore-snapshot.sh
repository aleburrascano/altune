#!/usr/bin/env bash

if [ $# -ne 2 ]; then
    echo "usage: restore-snapshot.sh <staging|prod> <dump file>" >&2
    exit 2
fi

DUMP_FILE=$(realpath -- "$2")
cd "$(dirname "$0")/.." || exit
. deploy/prod_host.sh

case "$1" in
    prod) MIGRATE_ENV_FILE="${PROD_MIGRATE_ENV_FILE:-.env.migrate.production}" ;;
    staging) MIGRATE_ENV_FILE="${STAGING_MIGRATE_ENV_FILE:-.env.migrate.staging}" ;;
    *)
        echo "usage: restore-snapshot.sh <staging|prod> <dump file>" >&2
        exit 2
        ;;
esac

if [ ! -f "$DUMP_FILE" ]; then
    log "FAILED: snapshot $DUMP_FILE not found"
    exit 1
fi

MIGRATE_DATABASE_URL=$(migrate_database_url "$MIGRATE_ENV_FILE") || exit 1

log "restoring $1 database from $DUMP_FILE"
pg_restore --clean --if-exists --no-owner --dbname "$MIGRATE_DATABASE_URL" "$DUMP_FILE"
