#!/usr/bin/env bash

if [ $# -ne 2 ]; then
    echo "usage: restore-snapshot.sh <staging|prod> <dump file>" >&2
    exit 2
fi

DUMP_FILE=$(realpath -- "$2")
cd "$(dirname "$0")/.." || exit
. deploy/prod_host.sh

case "$1" in
    prod) ENV_FILE="${PROD_ENV_FILE:-.env.production}" ;;
    staging) ENV_FILE="${STAGING_ENV_FILE:-.env.staging}" ;;
    *)
        echo "usage: restore-snapshot.sh <staging|prod> <dump file>" >&2
        exit 2
        ;;
esac

if [ ! -f "$ENV_FILE" ]; then
    log "FAILED: $ENV_FILE not found; cannot restore $1"
    exit 1
fi
if [ ! -f "$DUMP_FILE" ]; then
    log "FAILED: snapshot $DUMP_FILE not found"
    exit 1
fi

DATABASE_URL=$(read_env_var DATABASE_URL)
if [ -z "$DATABASE_URL" ]; then
    log "FAILED: DATABASE_URL is unset or empty in $ENV_FILE"
    exit 1
fi

log "restoring $1 database from $DUMP_FILE"
pg_restore --clean --if-exists --no-owner --dbname "$DATABASE_URL" "$DUMP_FILE"
