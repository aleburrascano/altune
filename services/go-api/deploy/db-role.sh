#!/usr/bin/env bash

set -euo pipefail

cd "$(dirname "$0")/.." || exit
. deploy/prod_host.sh

APP_ROLE=altune_app
GRANTS_FILE=migrations/052_app_role_grants.sql
USAGE="usage: db-role.sh <create|switch|unswitch|check|drop> <staging|production>"

[ $# -eq 2 ] || { echo "$USAGE" >&2; exit 2; }
ACTION=$1
TIER=$2
case "$TIER" in
    staging | production) ;;
    *) echo "$USAGE" >&2; exit 2 ;;
esac

APP_ENV_FILE=".env.$TIER"
MIGRATE_ENV_FILE=".env.migrate.$TIER"
BACKUP_GLOB="$APP_ENV_FILE.bak-approle-*"

url_user() {
    local rest=${1#*://}
    printf '%s' "${rest%%[:@]*}"
}

url_tail() {
    printf '%s' "${1#*@}"
}

url_scheme() {
    printf '%s' "${1%%://*}"
}

app_user_for() {
    local migrator_user suffix=""
    migrator_user=$(url_user "$1")
    case "$migrator_user" in
        *.*) suffix=".${migrator_user#*.}" ;;
    esac
    printf '%s%s' "$APP_ROLE" "$suffix"
}

migrator_psql() {
    psql -X -q -At -v ON_ERROR_STOP=1 "$MIGRATE_DATABASE_URL" "$@"
}

app_psql() {
    psql -X -q -At -v ON_ERROR_STOP=1 "$APP_URL" "$@"
}

ensure_migrate_env_file() {
    [ -f "$MIGRATE_ENV_FILE" ] && return 0
    if [ ! -f "$APP_ENV_FILE" ]; then
        log "FAILED: neither $MIGRATE_ENV_FILE nor $APP_ENV_FILE exists"
        exit 1
    fi
    local current
    current=$(ENV_FILE=$APP_ENV_FILE read_env_var DATABASE_URL)
    if [ -z "$current" ]; then
        log "FAILED: $MIGRATE_ENV_FILE is missing and $APP_ENV_FILE has no DATABASE_URL to seed it from"
        exit 1
    fi
    (umask 077 && printf 'MIGRATE_DATABASE_URL=%s\n' "$current" >"$MIGRATE_ENV_FILE")
    log "created $MIGRATE_ENV_FILE (mode 600) from the current $APP_ENV_FILE DATABASE_URL"
}

role_exists() {
    [ "$(migrator_psql -c "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '$APP_ROLE')")" = t ]
}

stored_app_url() {
    ENV_FILE=$MIGRATE_ENV_FILE read_env_var APP_DATABASE_URL
}

do_create() {
    ensure_migrate_env_file
    MIGRATE_DATABASE_URL=$(migrate_database_url "$MIGRATE_ENV_FILE") || exit 1
    require_psql
    command -v openssl >/dev/null || { log "FAILED: openssl not found on PATH"; exit 1; }
    APP_USER=$(app_user_for "$MIGRATE_DATABASE_URL")
    if role_exists; then
        if [ -z "$(stored_app_url)" ]; then
            log "FAILED: $APP_USER exists but $MIGRATE_ENV_FILE has no APP_DATABASE_URL; set its password with ALTER ROLE and store the URL by hand"
            exit 1
        fi
        log "$APP_USER already exists and $MIGRATE_ENV_FILE holds its URL"
    else
        create_role
    fi
    migrator_psql -f "$GRANTS_FILE" >/dev/null
    log "applied $GRANTS_FILE (idempotent); the deploy applies it again when it records the migration"
}

create_role() {
    local password
    password=$(openssl rand -hex 24)
    {
        printf "\\\\set pw '%s'\n" "$password"
        cat <<SQL
CREATE ROLE "$APP_ROLE" LOGIN NOSUPERUSER NOCREATEROLE NOCREATEDB NOREPLICATION BYPASSRLS PASSWORD :'pw';
SQL
    } | migrator_psql
    local entry
    entry=$(printf 'APP_DATABASE_URL=%s://%s:%s@%s' "$(url_scheme "$MIGRATE_DATABASE_URL")" "$APP_USER" "$password" \
        "$(url_tail "$MIGRATE_DATABASE_URL")")
    [ -z "$(tail -c1 "$MIGRATE_ENV_FILE")" ] || printf '\n' >>"$MIGRATE_ENV_FILE"
    printf '%s\n' "$entry" >>"$MIGRATE_ENV_FILE"
    log "created role $APP_USER; its URL is in $MIGRATE_ENV_FILE as APP_DATABASE_URL"
}

do_switch() {
    MIGRATE_DATABASE_URL=$(migrate_database_url "$MIGRATE_ENV_FILE") || exit 1
    local new_url backup tmp
    new_url=$(stored_app_url)
    if [ -z "$new_url" ]; then
        log "FAILED: $MIGRATE_ENV_FILE has no APP_DATABASE_URL; run 'db-role.sh create $TIER' first"
        exit 1
    fi
    [ -f "$APP_ENV_FILE" ] || { log "FAILED: $APP_ENV_FILE not found"; exit 1; }
    if [ "$(ENV_FILE=$APP_ENV_FILE read_env_var DATABASE_URL)" = "$new_url" ]; then
        log "$APP_ENV_FILE already uses the app role; nothing to do"
        return 0
    fi
    backup="$APP_ENV_FILE.bak-approle-$(date -u +%Y%m%dT%H%M%SZ)"
    (umask 077 && sed -E 's/^([[:space:]]*)DATABASE_URL=/\1#DATABASE_URL(pre-altune_app)=/' "$APP_ENV_FILE" >"$backup")
    tmp=$(umask 077 && mktemp "$APP_ENV_FILE.XXXXXX")
    NEW_URL=$new_url awk '
        /^[[:space:]]*DATABASE_URL=/ && !done { print "DATABASE_URL=" ENVIRON["NEW_URL"]; done = 1; next }
        { print }
        END { if (!done) exit 3 }
    ' "$APP_ENV_FILE" >"$tmp" || { rm -f "$tmp"; log "FAILED: no DATABASE_URL line in $APP_ENV_FILE"; exit 1; }
    chmod --reference="$APP_ENV_FILE" "$tmp"
    mv "$tmp" "$APP_ENV_FILE"
    log "$APP_ENV_FILE now uses the app role; the old line is commented out in $backup; release the running sha to pick it up"
}

do_unswitch() {
    local backup old backups
    backups=("$APP_ENV_FILE".bak-approle-*)
    backup=${backups[${#backups[@]} - 1]}
    if [ ! -f "$backup" ]; then
        log "FAILED: no $BACKUP_GLOB to restore"
        exit 1
    fi
    old=$(sed -nE 's/^[[:space:]]*#DATABASE_URL\(pre-altune_app\)=//p' "$backup" | head -1)
    if [ -z "$old" ]; then
        log "FAILED: $backup holds no pre-altune_app DATABASE_URL"
        exit 1
    fi
    local tmp
    tmp=$(umask 077 && mktemp "$APP_ENV_FILE.XXXXXX")
    NEW_URL=$old awk '
        /^[[:space:]]*DATABASE_URL=/ && !done { print "DATABASE_URL=" ENVIRON["NEW_URL"]; done = 1; next }
        { print }
        END { if (!done) exit 3 }
    ' "$APP_ENV_FILE" >"$tmp" || { rm -f "$tmp"; log "FAILED: no DATABASE_URL line in $APP_ENV_FILE"; exit 1; }
    chmod --reference="$APP_ENV_FILE" "$tmp"
    mv "$tmp" "$APP_ENV_FILE"
    log "$APP_ENV_FILE restored to the pre-altune_app DATABASE_URL from $backup; release the running sha to pick it up"
}

expect_fail() {
    local label=$1 sql=$2
    if app_psql -c "BEGIN; $sql; ROLLBACK;" >/dev/null 2>&1; then
        log "FAILED: $APP_ROLE could run: $label"
        FAILED=1
    else
        log "ok: $APP_ROLE cannot $label"
    fi
}

expect_value() {
    local label=$1 want=$2 got=$3
    if [ "$got" = "$want" ]; then
        log "ok: $label"
    else
        log "FAILED: $label (got '$got', want '$want')"
        FAILED=1
    fi
}

do_check() {
    MIGRATE_DATABASE_URL=$(migrate_database_url "$MIGRATE_ENV_FILE") || exit 1
    APP_URL=$(ENV_FILE=$APP_ENV_FILE read_env_var DATABASE_URL)
    [ -n "$APP_URL" ] || { log "FAILED: DATABASE_URL is unset in $APP_ENV_FILE"; exit 1; }
    require_psql
    FAILED=0
    APP_USER=$(app_user_for "$MIGRATE_DATABASE_URL")
    expect_value "$APP_ENV_FILE DATABASE_URL user is the app role" "$APP_USER" "$(url_user "$APP_URL")"
    expect_value "the app URL and the migrator URL differ" yes "$([ "$APP_URL" != "$MIGRATE_DATABASE_URL" ] && echo yes || echo no)"
    expect_value "the connection runs as the app role" "$APP_ROLE" "$(app_psql -c 'SELECT current_user')"
    expect_value "the app role can read tracks" t "$(app_psql -c 'SELECT count(*) >= 0 FROM public.tracks')"
    expect_fail "CREATE TABLE in public" "CREATE TABLE public.altune_ddl_probe (id int)"
    expect_fail "DROP TABLE tracks" "DROP TABLE public.tracks"
    expect_fail "ALTER TABLE tracks" "ALTER TABLE public.tracks ADD COLUMN altune_ddl_probe int"
    expect_fail "TRUNCATE tracks" "TRUNCATE public.tracks"
    expect_fail "write schema_migrations" "DELETE FROM public.schema_migrations"
    if [ "$(migrator_psql -c "SELECT to_regclass('auth.users') IS NOT NULL")" = t ]; then
        expect_value "the app role can read the auth_user_ids view" t "$(app_psql -c 'SELECT count(*) >= 0 FROM public.auth_user_ids')"
        expect_fail "read auth.users directly" "SELECT id FROM auth.users"
        expect_fail "write through auth_user_ids" "DELETE FROM public.auth_user_ids"
        expect_value "the app role holds only SELECT on auth_user_ids" SELECT \
            "$(migrator_psql -c "SELECT string_agg(privilege_type, ',' ORDER BY privilege_type) FROM information_schema.role_table_grants WHERE table_schema = 'public' AND table_name = 'auth_user_ids' AND grantee = '$APP_ROLE'")"
    fi
    expect_value "the app role is a member of no other role" 0 \
        "$(migrator_psql -c "SELECT count(*) FROM pg_auth_members m JOIN pg_roles r ON r.oid = m.member WHERE r.rolname = '$APP_ROLE'")"
    expect_value "the app role does not replicate" f \
        "$(migrator_psql -c "SELECT rolreplication FROM pg_roles WHERE rolname = '$APP_ROLE'")"
    expect_value "the app role bypasses row level security" t \
        "$(migrator_psql -c "SELECT rolbypassrls FROM pg_roles WHERE rolname = '$APP_ROLE'")"
    expect_value "no web role or service role can write through any view in public" 0 \
        "$(migrator_psql -c "SELECT count(*) FROM pg_class c, unnest(ARRAY['anon', 'authenticated', 'service_role']) w WHERE c.relnamespace = 'public'::regnamespace AND c.relkind = 'v' AND EXISTS (SELECT 1 FROM pg_roles WHERE rolname = w) AND (has_table_privilege(w, c.oid, 'INSERT') OR has_table_privilege(w, c.oid, 'UPDATE') OR has_table_privilege(w, c.oid, 'DELETE'))")"
    expect_value "the app role has no superuser, createrole, createdb" "f|f|f" \
        "$(migrator_psql -c "SELECT rolsuper, rolcreaterole, rolcreatedb FROM pg_roles WHERE rolname = '$APP_ROLE'")"
    expect_value "the app role owns no table, sequence or function" 0 \
        "$(migrator_psql -c "SELECT (SELECT count(*) FROM pg_class WHERE relowner = r.oid) + (SELECT count(*) FROM pg_proc WHERE proowner = r.oid) FROM pg_roles r WHERE r.rolname = '$APP_ROLE'")"
    expect_value "anon and authenticated hold no table or sequence privilege in public" 0 \
        "$(migrator_psql -c "SELECT count(*) FROM pg_class c, unnest(ARRAY['anon', 'authenticated']) w WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'v', 'S') AND EXISTS (SELECT 1 FROM pg_roles WHERE rolname = w) AND (CASE c.relkind WHEN 'S' THEN has_sequence_privilege(w, c.oid, 'USAGE, SELECT, UPDATE') ELSE has_any_column_privilege(w, c.oid, 'SELECT, INSERT, UPDATE, REFERENCES') OR has_table_privilege(w, c.oid, 'DELETE, TRUNCATE, TRIGGER') END)")"
    expect_value "row level security is on for every public table" 0 \
        "$(migrator_psql -c "SELECT count(*) FROM pg_tables WHERE schemaname = 'public' AND NOT rowsecurity")"
    [ "$FAILED" = 0 ] || { log "FAILED: db role check on $TIER"; exit 1; }
    log "db role check passed on $TIER"
}

do_drop() {
    MIGRATE_DATABASE_URL=$(migrate_database_url "$MIGRATE_ENV_FILE") || exit 1
    require_psql
    APP_USER=$(app_user_for "$MIGRATE_DATABASE_URL")
    if [ "$(ENV_FILE=$APP_ENV_FILE read_env_var DATABASE_URL | sed -E 's#^[a-z]+://([^:@]*).*#\1#')" = "$APP_USER" ]; then
        log "FAILED: $APP_ENV_FILE still uses $APP_USER; run 'db-role.sh unswitch $TIER' and release first"
        exit 1
    fi
    if ! role_exists; then
        log "$APP_USER does not exist; nothing to drop"
        return 0
    fi
    migrator_psql <<SQL
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM "$APP_ROLE";
REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM "$APP_ROLE";
REVOKE USAGE ON SCHEMA public FROM "$APP_ROLE";
ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON TABLES FROM "$APP_ROLE";
ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON SEQUENCES FROM "$APP_ROLE";
DROP ROLE "$APP_ROLE";
SQL
    local tmp
    tmp=$(umask 077 && mktemp "$MIGRATE_ENV_FILE.XXXXXX")
    grep -v '^APP_DATABASE_URL=' "$MIGRATE_ENV_FILE" >"$tmp" || true
    mv "$tmp" "$MIGRATE_ENV_FILE"
    log "dropped role $APP_USER on $TIER"
}

case "$ACTION" in
    create) do_create ;;
    switch) do_switch ;;
    unswitch) do_unswitch ;;
    check) do_check ;;
    drop) do_drop ;;
    *) echo "$USAGE" >&2; exit 2 ;;
esac
