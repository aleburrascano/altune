#!/usr/bin/env bash


cd "$(dirname "$0")/.." || exit
. deploy/lib.sh

PROD_ENV="${PROD_ENV_FILE:-.env.production}"
STAGING_ENV="${STAGING_ENV_FILE:-.env.staging}"
IMPORT_SCHEMA=prod_import

MANIFEST=deploy/staging-sync.tables
MANIFEST_NAME=staging-sync.tables
TABLE_NAME_RE='^[a-z_0-9]+$'

declare -a MANIFEST_TABLES=()
declare -A TABLE_RULE TABLE_PARENT TABLE_FK_COLUMN

UUID_RE='^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'

env_value() {
    ENV_FILE=$1 read_env_var "$2"
}

require_env_file() {
    if [ ! -f "$1" ]; then
        log "FAILED: $1 not found; cannot sync staging from prod"
        exit 1
    fi
}

prod_read() {
    { printf 'BEGIN READ ONLY;\n'; cat; printf 'COMMIT;\n'; } |
        psql -X -q -At -v ON_ERROR_STOP=1 "$@" "$PROD_URL"
}

staging_sql() {
    psql -X -q -At -v ON_ERROR_STOP=1 "$@" "$STAGING_URL"
}

cleanup_import() {
    staging_sql -c "DROP SCHEMA IF EXISTS $IMPORT_SCHEMA CASCADE" >/dev/null 2>&1 || true
}

columns_of() {
    printf '%s\n' "SELECT column_name FROM information_schema.columns WHERE table_schema = 'public' AND table_name = '$1' AND is_generated = 'NEVER' ORDER BY ordinal_position;"
}

shared_columns() {
    local table=$1 prod_cols col out=""
    prod_cols=$(columns_of "$table" | prod_read)
    for col in $(columns_of "$table" | staging_sql); do
        if printf '%s\n' "$prod_cols" | grep -qx "$col"; then
            out="${out:+$out,}\"$col\""
        fi
    done
    printf '%s' "$out"
}

manifest_fail() {
    log "FAILED: $MANIFEST_NAME line $1: $2"
    exit 1
}

load_manifest() {
    local line_no=0 table rule rest parent fk_column extra
    while read -r table rule rest; do
        line_no=$((line_no + 1))
        if [ -z "$table" ] || [[ $table == \#* ]]; then
            continue
        fi
        [[ $table =~ $TABLE_NAME_RE ]] || manifest_fail "$line_no" "bad table name '$table'"
        [ -z "${TABLE_RULE[$table]:-}" ] || manifest_fail "$line_no" "public.$table is listed twice"
        case $rule in
            user | global | reset) ;;
            skip) [ -n "$rest" ] || manifest_fail "$line_no" "skip needs a reason" ;;
            child)
                read -r parent fk_column extra <<<"$rest"
                [[ $parent =~ $TABLE_NAME_RE && $fk_column =~ $TABLE_NAME_RE && -z "$extra" ]] ||
                    manifest_fail "$line_no" "child needs '<parent-table> <fk-col>'"
                [ "${TABLE_RULE[$parent]:-}" = user ] ||
                    manifest_fail "$line_no" "child parent '$parent' must be a user table listed above it"
                TABLE_PARENT[$table]=$parent
                TABLE_FK_COLUMN[$table]=$fk_column ;;
            *) manifest_fail "$line_no" "unknown rule '$rule'" ;;
        esac
        TABLE_RULE[$table]=$rule
        MANIFEST_TABLES+=("$table")
    done <"$MANIFEST"
}

tables_with_rule() {
    local table
    for table in "${MANIFEST_TABLES[@]}"; do
        if [ "${TABLE_RULE[$table]}" = "$1" ]; then
            printf '%s\n' "$table"
        fi
    done
}

public_tables_sql() {
    printf '%s\n' "SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE' ORDER BY table_name;"
}

require_every_table_has_a_rule() {
    local live_tables table
    live_tables=$({ public_tables_sql | prod_read; public_tables_sql | staging_sql; } | sort -u)
    for table in $live_tables; do
        if [ -z "${TABLE_RULE[$table]:-}" ]; then
            log "FAILED: public.$table has no rule in $MANIFEST_NAME"
            exit 1
        fi
    done
    for table in "${MANIFEST_TABLES[@]}"; do
        if ! printf '%s\n' "$live_tables" | grep -qx "$table"; then
            log "FAILED: $MANIFEST_NAME lists public.$table, which exists on neither tier"
            exit 1
        fi
    done
}

synced_tables() {
    tables_with_rule user
    tables_with_rule child
    tables_with_rule global
}

source_filter() {
    case ${TABLE_RULE[$1]} in
        child)
            printf '%s IN (SELECT id FROM public.%s WHERE user_id = ANY(%s))' \
                "${TABLE_FK_COLUMN[$1]}" "${TABLE_PARENT[$1]}" "$PROD_IDS" ;;
        global)
            printf 'true' ;;
        *)
            printf 'user_id = ANY(%s)' "$PROD_IDS" ;;
    esac
}

import_table() {
    local table=$1 cols=$2
    staging_sql -c "CREATE TABLE $IMPORT_SCHEMA.$table AS SELECT $cols FROM public.$table WITH NO DATA"
    printf '%s\n' "\\copy (SELECT $cols FROM public.$table WHERE $(source_filter "$table")) TO STDOUT" |
        prod_read |
        staging_sql -c "\\copy $IMPORT_SCHEMA.$table ($cols) FROM pstdin"
}

swap_sql() {
    local table cols select_cols
    for table in $(tables_with_rule user) $(tables_with_rule reset); do
        printf 'DELETE FROM public.%s WHERE user_id IN (SELECT staging_id FROM %s.user_map);\n' \
            "$table" "$IMPORT_SCHEMA"
    done
    for table in $(tables_with_rule global); do
        printf 'DELETE FROM public.%s;\n' "$table"
    done
    for table in $(tables_with_rule user); do
        cols=${TABLE_COLUMNS[$table]}
        select_cols=$(printf '%s' "$cols" | sed -E 's/"([a-z_0-9]+)"/i."\1"/g; s/i\."user_id"/m.staging_id/')
        printf 'INSERT INTO public.%s (%s) SELECT %s FROM %s.%s i JOIN %s.user_map m ON m.prod_id = i.user_id;\n' \
            "$table" "$cols" "$select_cols" "$IMPORT_SCHEMA" "$table" "$IMPORT_SCHEMA"
    done
    for table in $(tables_with_rule child) $(tables_with_rule global); do
        cols=${TABLE_COLUMNS[$table]}
        printf 'INSERT INTO public.%s (%s) SELECT %s FROM %s.%s;\n' \
            "$table" "$cols" "$cols" "$IMPORT_SCHEMA" "$table"
    done
}

require_env_file "$PROD_ENV"
require_env_file "$STAGING_ENV"
PROD_URL=$(env_value "$PROD_ENV" DATABASE_URL)
STAGING_URL=$(env_value "$STAGING_ENV" DATABASE_URL)
if [ -z "$PROD_URL" ] || [ -z "$STAGING_URL" ]; then
    log "FAILED: DATABASE_URL is unset or empty in $PROD_ENV or $STAGING_ENV"
    exit 1
fi
PROD_PROJECT=$(env_value "$PROD_ENV" SUPABASE_PROJECT_URL)
STAGING_PROJECT=$(env_value "$STAGING_ENV" SUPABASE_PROJECT_URL)
if [ "$PROD_URL" = "$STAGING_URL" ] || { [ -n "$PROD_PROJECT" ] && [ "$PROD_PROJECT" = "$STAGING_PROJECT" ]; }; then
    log "FAILED: $STAGING_ENV points at the prod database; refusing to overwrite it"
    exit 1
fi
require_psql
load_manifest
require_every_table_has_a_rule

staging_accounts=$(staging_sql -c "SELECT id, lower(email) FROM auth.users WHERE email IS NOT NULL")
prod_accounts=$(printf '%s\n' "SELECT id, lower(email) FROM auth.users WHERE lower(email) = ANY(string_to_array(:'emails', E'\\n'));" |
    prod_read -v emails="$(printf '%s\n' "$staging_accounts" | cut -d'|' -f2)")

USER_MAP=$(awk -F'|' 'NR == FNR { staging[$2] = $1; next } ($2 in staging) { print $1 "|" staging[$2] }' \
    <(printf '%s\n' "$staging_accounts") <(printf '%s\n' "$prod_accounts"))
if [ -z "$USER_MAP" ]; then
    log "FAILED: no prod account has a staging twin (matched by email); nothing to sync"
    exit 1
fi
while IFS='|' read -r prod_id staging_id; do
    if ! [[ $prod_id =~ $UUID_RE && $staging_id =~ $UUID_RE ]]; then
        log "FAILED: account map holds a non-UUID id; refusing to build SQL from it"
        exit 1
    fi
done <<<"$USER_MAP"
PROD_IDS="'{$(printf '%s\n' "$USER_MAP" | cut -d'|' -f1 | paste -sd,)}'::uuid[]"
log "syncing $(printf '%s\n' "$USER_MAP" | wc -l | tr -d ' ') matched account(s) from prod"

prod_api=$(docker ps --filter "name=^altune-go-api-" --format '{{.Names}}' | head -1)
if [ -z "$prod_api" ]; then
    log "FAILED: no running altune-go-api-* container; cannot promote staging audio"
    exit 1
fi
if ! STAGING_DATABASE_URL="$STAGING_URL" docker exec -e STAGING_DATABASE_URL "$prod_api" /app promote-staging --execute; then
    log "FAILED: promote-staging failed in $prod_api; aborting sync so its unpromoted songs are not wiped"
    exit 1
fi

trap cleanup_import EXIT
cleanup_import
staging_sql -c "CREATE SCHEMA $IMPORT_SCHEMA" \
    -c "CREATE TABLE $IMPORT_SCHEMA.user_map (prod_id uuid PRIMARY KEY, staging_id uuid NOT NULL)"
printf '%s\n' "$USER_MAP" | staging_sql -c "\\copy $IMPORT_SCHEMA.user_map FROM pstdin WITH (DELIMITER '|')"

declare -A TABLE_COLUMNS
for table in $(synced_tables); do
    TABLE_COLUMNS[$table]=$(shared_columns "$table")
    if [ -z "${TABLE_COLUMNS[$table]}" ]; then
        log "FAILED: public.$table is missing on one tier"
        exit 1
    fi
    import_table "$table" "${TABLE_COLUMNS[$table]}"
done

swap_sql | staging_sql --single-transaction >/dev/null
for table in $(synced_tables); do
    log "  $table: $(staging_sql -c "SELECT count(*) FROM $IMPORT_SCHEMA.$table") rows from prod"
done
log "staging synced from prod"

staging_api=$(docker ps --filter "name=^altune-staging-go-api-" --format '{{.Names}}' | head -1)
if [ -z "$staging_api" ]; then
    log "WARNING: no running altune-staging-go-api-* container; skipping sweep-staging-audio"
elif ! docker exec "$staging_api" /app sweep-staging-audio --execute; then
    log "WARNING: sweep-staging-audio failed in $staging_api"
fi
