#!/usr/bin/env bash

# Refresh the STAGING Supabase DB with prod's data so staging runs against the real
# library instead of an empty one. One-way, prod -> staging. Run nightly and on
# demand by .github/workflows/staging-sync.yml, or by hand on the VM:
#   cd /home/ubuntu/altune/services/go-api && bash deploy/staging-sync.sh
#
# The database replace below still only ever reads prod: every prod query runs
# inside BEGIN READ ONLY, so the server itself rejects a write even if this
# script is wrong (the Supabase pooler drops PGOPTIONS, so the read-only flag
# has to ride in the transaction). Prod is no longer read-only overall, though:
# before the replace, `promote-staging` (#3092) runs INSERT-only in the prod
# go-api container to land any song a user kept on staging into prod, so the
# nightly replace below never wipes a download nobody asked to lose.
#
# Staging is its own Supabase auth realm (design Decision 2), so a prod user UUID
# means nothing there. Accounts are matched by email: a prod account that also
# exists in staging has its rows copied with user_id rewritten to its staging UUID.
# A prod account with no staging twin is filtered out at the source, so its rows
# never leave prod.
#
# For matched accounts the staging copy is replaced wholesale; staging-only
# accounts (smoke users) are left alone. The replace is one transaction, so any
# failure leaves staging exactly as it was. Only the columns both tiers share are
# copied, so staging running a migration ahead of prod is fine, unless that
# migration adds a NOT NULL column with no default, which fails the run loudly.
#
# Audio objects are not copied by the database replace: staging reads prod's
# bucket with a key scoped to the `staging/` prefix (RUNBOOK.md, "Staging data
# from prod"), so copied tracks play and a staging delete cannot reach prod
# audio. orphaned_audio (a queue of storage deletes) and schema_migrations are
# never copied. After a successful replace, `sweep-staging-audio` (#3092) runs
# in the staging container to delete `staging/` objects no staging track
# references anymore and that are over an hour old.
#
# Neither DATABASE_URL is ever logged; both are grepped, never `source`d.

cd "$(dirname "$0")/.." || exit
. deploy/lib.sh

PROD_ENV="${PROD_ENV_FILE:-.env.production}"
STAGING_ENV="${STAGING_ENV_FILE:-.env.staging}"
IMPORT_SCHEMA=prod_import

# Insert order: parents before the children that reference them.
USER_TABLES="featured_artists tracks playlists discovery_events discovery_favorites discovery_search_clicks discovery_search_history playback_queue_state"
CHILD_TABLES="playlist_tracks track_featured_artists acquisition_cooldowns"
GLOBAL_TABLES="entity_identity"

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

# Columns present on both tiers, in staging's order, each double-quoted. Generated
# columns are left out: Postgres computes them and rejects an explicit value.
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

# The prod-side WHERE that keeps a table to the matched accounts' rows.
source_filter() {
    case $1 in
        playlist_tracks)
            printf 'playlist_id IN (SELECT id FROM public.playlists WHERE user_id = ANY(%s))' "$PROD_IDS" ;;
        track_featured_artists | acquisition_cooldowns)
            printf 'track_id IN (SELECT id FROM public.tracks WHERE user_id = ANY(%s))' "$PROD_IDS" ;;
        entity_identity)
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

# One transaction: drop the matched accounts' staging rows (children cascade),
# then insert prod's rows with user_id rewritten through the account map.
swap_sql() {
    local table cols select_cols
    for table in $USER_TABLES orphaned_audio; do
        printf 'DELETE FROM public.%s WHERE user_id IN (SELECT staging_id FROM %s.user_map);\n' \
            "$table" "$IMPORT_SCHEMA"
    done
    for table in $GLOBAL_TABLES; do
        printf 'DELETE FROM public.%s;\n' "$table"
    done
    for table in $USER_TABLES; do
        cols=${TABLE_COLUMNS[$table]}
        select_cols=$(printf '%s' "$cols" | sed -E 's/"([a-z_0-9]+)"/i."\1"/g; s/i\."user_id"/m.staging_id/')
        printf 'INSERT INTO public.%s (%s) SELECT %s FROM %s.%s i JOIN %s.user_map m ON m.prod_id = i.user_id;\n' \
            "$table" "$cols" "$select_cols" "$IMPORT_SCHEMA" "$table" "$IMPORT_SCHEMA"
    done
    for table in $CHILD_TABLES $GLOBAL_TABLES; do
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

staging_accounts=$(staging_sql -c "SELECT id, lower(email) FROM auth.users WHERE email IS NOT NULL")
prod_accounts=$(printf '%s\n' "SELECT id, lower(email) FROM auth.users WHERE lower(email) = ANY(string_to_array(:'emails', E'\\n'));" |
    prod_read -v emails="$(printf '%s\n' "$staging_accounts" | cut -d'|' -f2)")

# prod_id|staging_id for every email present in both realms.
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

# A song kept on staging must land in prod before the replace below wipes it
# (#3092): promote-staging runs INSERT-only in the running prod container, so a
# failure here aborts the whole sync rather than silently losing the download.
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
for table in $USER_TABLES $CHILD_TABLES $GLOBAL_TABLES; do
    TABLE_COLUMNS[$table]=$(shared_columns "$table")
    if [ -z "${TABLE_COLUMNS[$table]}" ]; then
        log "FAILED: public.$table is missing on one tier"
        exit 1
    fi
    import_table "$table" "${TABLE_COLUMNS[$table]}"
done

swap_sql | staging_sql --single-transaction >/dev/null
for table in $USER_TABLES $CHILD_TABLES $GLOBAL_TABLES; do
    log "  $table: $(staging_sql -c "SELECT count(*) FROM $IMPORT_SCHEMA.$table") rows from prod"
done
log "staging synced from prod"

# Best-effort cleanup of staging/ objects the promote above (or an abandoned
# acquisition) left behind. A failure here only warns: it never undoes a
# replace that already succeeded.
staging_api=$(docker ps --filter "name=^altune-staging-go-api-" --format '{{.Names}}' | head -1)
if [ -z "$staging_api" ]; then
    log "WARNING: no running altune-staging-go-api-* container; skipping sweep-staging-audio"
elif ! docker exec "$staging_api" /app sweep-staging-audio --execute; then
    log "WARNING: sweep-staging-audio failed in $staging_api"
fi
