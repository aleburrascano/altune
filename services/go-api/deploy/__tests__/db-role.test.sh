#!/usr/bin/env bash

set -uo pipefail

HERE=$(cd "$(dirname "$0")/.." && pwd)
FAILURES=0
RUN_ID="db-role-test-$$"
IMAGE=postgres:17
WORK=$(mktemp -d)
REF=testref
MIGRATOR=migrator
APP=altune_app

cleanup() {
    docker rm -fv "$RUN_ID" >/dev/null 2>&1
    rm -rf "$WORK"
}
trap cleanup EXIT

as_migrator() {
    docker exec -i -e PGPASSWORD=pw "$RUN_ID" psql -X -q -At -v ON_ERROR_STOP=1 -h 127.0.0.1 -U "$MIGRATOR" -d app "$@"
}

apply_052() {
    docker exec -i -e PGPASSWORD=pw "$RUN_ID" psql -X -q -v ON_ERROR_STOP=1 -h 127.0.0.1 -U "$MIGRATOR" -d app -f /api/migrations/052_app_role_grants.sql >/dev/null
}

in_vm() {
    docker exec -u "$(id -u)" -w /api "$RUN_ID" bash deploy/db-role.sh "$@" >"$WORK/out" 2>&1
}

check() {
    local label=$1 want=$2 got=$3
    if [ "$got" != "$want" ]; then
        printf 'FAIL: %s\n      want [%s], got [%s]\n' "$label" "$want" "$got"
        FAILURES=$((FAILURES + 1))
    fi
}

check_out() {
    grep -qF -- "$2" "$WORK/out" || {
        printf 'FAIL: %s\n      expected output to mention [%s]; output:\n' "$1" "$2"
        sed 's/^/        /' "$WORK/out"
        FAILURES=$((FAILURES + 1))
    }
}

app_password() {
    sed -nE 's#^APP_DATABASE_URL=[a-z]+://[^:]+:([^@]+)@.*#\1#p' "$WORK/api/.env.migrate.staging"
}

leaked_secret() {
    local password
    password=$(app_password)
    [ -n "$password" ] || { echo "no-password-found"; return; }
    grep -cF -- "$password" "$WORK/out"
}

as_app() {
    docker exec -i -e PGPASSWORD="$(app_password)" "$RUN_ID" psql -X -q -At -h 127.0.0.1 -U "$APP" -d app "$@" 2>&1
}

start_postgres() {
    mkdir -p "$WORK/api/deploy"
    cp "$HERE/prod_host.sh" "$HERE/db-role.sh" "$WORK/api/deploy/"
    cp -r "$HERE/../migrations" "$WORK/api/migrations"
    docker run -d --label altune-ci=1 --name "$RUN_ID" -e POSTGRES_PASSWORD=pw \
        -v "$WORK/api:/api" "$IMAGE" >/dev/null || exit 1
    local attempt
    for attempt in $(seq 1 60); do
        docker exec "$RUN_ID" pg_isready -q -U postgres -h 127.0.0.1 && return 0
        sleep 0.5
    done
    printf 'FAIL: postgres never became ready\n'
    exit 1
}

build_supabase_like_schema() {
    docker exec -i "$RUN_ID" psql -X -q -At -v ON_ERROR_STOP=1 -U postgres <<SQL || exit 1
CREATE ROLE anon NOLOGIN;
CREATE ROLE authenticated NOLOGIN;
CREATE ROLE "$MIGRATOR" LOGIN CREATEROLE CREATEDB BYPASSRLS PASSWORD 'pw';
CREATE DATABASE app OWNER "$MIGRATOR";
SQL
    docker exec -i "$RUN_ID" psql -X -q -At -v ON_ERROR_STOP=1 -U postgres -d app <<SQL || exit 1
CREATE ROLE auth_owner NOLOGIN;
CREATE SCHEMA auth AUTHORIZATION auth_owner;
CREATE TABLE auth.users (id uuid PRIMARY KEY, email text);
ALTER TABLE auth.users OWNER TO auth_owner;
INSERT INTO auth.users VALUES ('11111111-1111-1111-1111-111111111111', 'someone@example.com');
GRANT USAGE ON SCHEMA auth TO "$MIGRATOR";
GRANT SELECT ON auth.users TO "$MIGRATOR";
SQL
    local file
    as_migrator -c "CREATE TABLE schema_migrations (version text PRIMARY KEY)" || exit 1
    as_migrator -c "ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO anon, authenticated" || exit 1
    for file in $(cd "$WORK/api" && printf '%s\n' migrations/*.sql | sort -V); do
        docker exec -i -e PGPASSWORD=pw "$RUN_ID" psql -X -q -v ON_ERROR_STOP=1 -h 127.0.0.1 -U "$MIGRATOR" -d app -f "/api/$file" >/dev/null ||
            { printf 'FAIL: %s did not apply\n' "$file"; exit 1; }
    done
}

write_app_env() {
    printf 'PORT=8000\nDATABASE_URL=postgresql://%s:pw@127.0.0.1:5432/app?sslmode=disable\nOTHER=keep-me\n' "$MIGRATOR" \
        >"$WORK/api/.env.staging"
    chmod 600 "$WORK/api/.env.staging"
}

test_create() {
    write_app_env
    in_vm create staging
    check "create succeeds" 0 "$?"
    check_out "create says what it made" "created role altune_app"
    check "the app role exists" t "$(as_migrator -c "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '$APP')")"
    check "the app role is a plain login that bypasses RLS" "t|f|f|f|t" \
        "$(as_migrator -c "SELECT rolcanlogin, rolsuper, rolcreaterole, rolcreatedb, rolbypassrls FROM pg_roles WHERE rolname = '$APP'")"
    check "the migrator file is mode 600" 600 "$(stat -c %a "$WORK/api/.env.migrate.staging")"
    check "the migrator file is seeded with the old URL" 1 "$(grep -c "^MIGRATE_DATABASE_URL=postgresql://$MIGRATOR:pw@" "$WORK/api/.env.migrate.staging")"
    check "the app URL is stored for the switch" 1 "$(grep -c "^APP_DATABASE_URL=postgresql://$APP:" "$WORK/api/.env.migrate.staging")"
    check "the password never reaches the output" 0 "$(leaked_secret)"
    check "the app env is untouched by create" 1 "$(grep -c "^DATABASE_URL=postgresql://$MIGRATOR:" "$WORK/api/.env.staging")"

    local before after
    before=$(grep '^APP_DATABASE_URL=' "$WORK/api/.env.migrate.staging")
    in_vm create staging
    check "a second create succeeds" 0 "$?"
    check_out "a second create is a no-op" "nothing to do"
    after=$(grep '^APP_DATABASE_URL=' "$WORK/api/.env.migrate.staging")
    check "a second create keeps the password" "$before" "$after"
}

test_grants_then_switch_then_check() {
    apply_052
    check "052 applies once the role exists" 0 "$?"
    apply_052
    check "052 is idempotent" 0 "$?"

    in_vm check staging
    check "check refuses while the app still runs as the migrator" 1 "$?"
    check_out "the refusal names the user" "DATABASE_URL user is the app role"

    in_vm switch staging
    check "switch succeeds" 0 "$?"
    check "the app env has the app URL" 1 "$(grep -c "^DATABASE_URL=postgresql://$APP:" "$WORK/api/.env.staging")"
    check "the app env keeps its other lines" 2 "$(grep -cE '^(PORT=8000|OTHER=keep-me)$' "$WORK/api/.env.staging")"
    check "the app env keeps its mode" 600 "$(stat -c %a "$WORK/api/.env.staging")"
    local backup
    backup=$(ls "$WORK"/api/.env.staging.bak-approle-* | head -1)
    check "the backup comments out the old URL" 1 "$(grep -c "^#DATABASE_URL(pre-altune_app)=postgresql://$MIGRATOR:" "$backup")"
    check "the backup has no live DATABASE_URL" 0 "$(grep -c '^DATABASE_URL=' "$backup")"
    check "the backup is mode 600" 600 "$(stat -c %a "$backup")"
    check "the password never reaches the switch output" 0 "$(leaked_secret)"
    in_vm switch staging
    check_out "a second switch is a no-op" "nothing to do"
    check "a second switch makes no second backup" 1 "$(ls "$WORK"/api/.env.staging.bak-approle-* | wc -l | tr -d ' ')"

    in_vm check staging
    check "check passes for the app role" 0 "$?"
    check_out "check proves the DDL denial" "cannot CREATE TABLE in public"
    check_out "check ends green" "db role check passed on staging"
}

test_app_rights() {
    as_migrator -c "CREATE TABLE later_table (id int PRIMARY KEY)"
    check "a table created after the grants is writable by the app role" "1" \
        "$(as_app -c 'INSERT INTO later_table VALUES (1); SELECT count(*) FROM later_table' | tail -1)"
    check "the app role reads user ids through the view" "11111111-1111-1111-1111-111111111111" \
        "$(as_app -c 'SELECT id FROM public.auth_user_ids')"
    check "the view exposes no email column" "ERROR" "$(as_app -c 'SELECT email FROM public.auth_user_ids' | cut -d: -f1 | head -1)"
    check "the app role cannot read auth.users directly" "ERROR" "$(as_app -c 'SELECT id FROM auth.users' | cut -d: -f1 | head -1)"
    check "the app role cannot delete accounts through the view" "ERROR" \
        "$(as_app -c 'DELETE FROM public.auth_user_ids' | cut -d: -f1 | head -1)"
    check "no account was deleted" 1 "$(docker exec -i "$RUN_ID" psql -X -q -At -U postgres -d app -c 'SELECT count(*) FROM auth.users')"
    check "the app role cannot DROP" "ERROR" "$(as_app -c 'DROP TABLE later_table' | cut -d: -f1 | head -1)"
    check "the app role cannot read the migration tracker" "ERROR" "$(as_app -c 'SELECT * FROM schema_migrations' | cut -d: -f1 | head -1)"
    check "anon holds nothing on a table created after 052" 0 \
        "$(as_migrator -c "SELECT count(*) FROM information_schema.role_table_grants WHERE grantee = 'anon' AND table_name = 'later_table'")"
}

test_check_catches_a_broken_grant() {
    in_vm check staging
    check "check flags a table created without row level security" 1 "$?"
    check_out "the failure is the RLS rule" "row level security is on for every public table"
    as_migrator -c "ALTER TABLE later_table ENABLE ROW LEVEL SECURITY"
    in_vm check staging
    check "check passes once row level security is on" 0 "$?"

    as_migrator -c "GRANT CREATE ON SCHEMA public TO \"$APP\""
    in_vm check staging
    check "check fails when the app role may create tables" 1 "$?"
    check_out "the failure names the DDL" "could run: CREATE TABLE in public"
    as_migrator -c "REVOKE CREATE ON SCHEMA public FROM \"$APP\""
    as_migrator -c "GRANT SELECT ON public.tracks TO anon"
    in_vm check staging
    check "check fails when anon holds a table privilege" 1 "$?"
    as_migrator -c "REVOKE ALL ON public.tracks FROM anon"
    in_vm check staging
    check "check passes again once repaired" 0 "$?"
}

test_unswitch_and_drop() {
    in_vm drop staging
    check "drop is refused while the app env uses the role" 1 "$?"
    check_out "the refusal says how to back out" "unswitch staging"

    in_vm unswitch staging
    check "unswitch succeeds" 0 "$?"
    check "unswitch restores the old URL" 1 "$(grep -c "^DATABASE_URL=postgresql://$MIGRATOR:pw@" "$WORK/api/.env.staging")"
    check "unswitch keeps the other lines" 2 "$(grep -cE '^(PORT=8000|OTHER=keep-me)$' "$WORK/api/.env.staging")"

    in_vm drop staging
    check "drop succeeds after the unswitch" 0 "$?"
    check "the role is gone" f "$(as_migrator -c "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '$APP')")"
    check "the stored app URL is gone" 0 "$(grep -c '^APP_DATABASE_URL=' "$WORK/api/.env.migrate.staging")"
    check "the migrator URL stays" 1 "$(grep -c '^MIGRATE_DATABASE_URL=' "$WORK/api/.env.migrate.staging")"
    in_vm drop staging
    check_out "a second drop is a no-op" "nothing to drop"
    check "the tables are untouched" t "$(as_migrator -c "SELECT count(*) > 20 FROM pg_tables WHERE schemaname = 'public'")"
}

test_pooler_suffix_follows_the_migrator_user() {
    as_migrator -c "CREATE ROLE \"mig.$REF\" LOGIN CREATEROLE BYPASSRLS PASSWORD 'pw'"
    printf 'DATABASE_URL=postgresql://mig.%s:pw@127.0.0.1:5432/app\n' "$REF" >"$WORK/api/.env.production"
    chmod 600 "$WORK/api/.env.production"
    in_vm create production
    check "create works through a suffixed migrator login" 0 "$?"
    check "the stored app login carries the same suffix" 1 \
        "$(grep -c "^APP_DATABASE_URL=postgresql://altune_app.$REF:" "$WORK/api/.env.migrate.production")"
    check "the role itself has no suffix" t "$(as_migrator -c "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'altune_app')")"
}

test_usage_errors() {
    in_vm create nowhere
    check "an unknown tier is refused" 2 "$?"
    in_vm frobnicate staging
    check "an unknown action is refused" 2 "$?"
    rm -f "$WORK/api/.env.migrate.staging" "$WORK/api/.env.staging"
    in_vm create staging
    check "create without any env file fails loudly" 1 "$?"
}

start_postgres
build_supabase_like_schema
test_create
test_grants_then_switch_then_check
test_app_rights
test_check_catches_a_broken_grant
test_unswitch_and_drop
test_pooler_suffix_follows_the_migrator_user
test_usage_errors

if [ "$FAILURES" -gt 0 ]; then
    printf '%s db-role check(s) failed\n' "$FAILURES"
    exit 1
fi
echo "all db-role checks passed"
