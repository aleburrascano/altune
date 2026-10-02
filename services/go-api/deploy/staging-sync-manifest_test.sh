#!/usr/bin/env bash


set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
FAILURES=0
RUN_ID="staging-sync-manifest-test-$$"
IMAGE=postgres:17
WORK=$(mktemp -d)
MANIFEST="$HERE/staging-sync.tables"

cleanup() {
    docker rm -fv "$RUN_ID" >/dev/null 2>&1
    rm -rf "$WORK"
}
trap cleanup EXIT

sql() {
    docker exec -i "$RUN_ID" psql -X -q -At -v ON_ERROR_STOP=1 -U postgres -d "$1" "${@:2}"
}

start_postgres() {
    cp -r "$HERE/../migrations" "$WORK/migrations"
    docker run -d --label altune-ci=1 --name "$RUN_ID" -e POSTGRES_PASSWORD=pw \
        -v "$WORK:/api" "$IMAGE" >/dev/null || exit 1
    local attempt
    for attempt in $(seq 1 60); do
        docker exec "$RUN_ID" pg_isready -q -U postgres -h 127.0.0.1 && return 0
        sleep 0.5
    done
    printf 'FAIL: postgres never became ready (%s tries)\n' "$attempt"
    exit 1
}

migrate_fresh_database() {
    local file
    sql postgres -c "DROP DATABASE IF EXISTS fresh" -c "CREATE DATABASE fresh"
    sql fresh -c "CREATE SCHEMA auth" -c "CREATE TABLE auth.users (id uuid PRIMARY KEY, email text)" \
        -c "CREATE TABLE schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())"
    for file in $(cd "$WORK" && printf '%s\n' migrations/*.sql | sort -V); do
        sql fresh -f "/api/$file" >/dev/null || { printf 'FAIL: %s did not apply\n' "$file"; exit 1; }
    done
}

public_tables() {
    sql fresh -c "SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE'" | sort
}

manifest_tables() {
    awk '$1 != "" && $1 !~ /^#/ { print $1 }' "$MANIFEST" | sort
}

manifest_gaps() {
    comm -23 <(public_tables) <(manifest_tables) | awk '{ print "public." $1 " has no line in staging-sync.tables" }'
    comm -13 <(public_tables) <(manifest_tables) | awk '{ print "staging-sync.tables line names " $1 ", which no migration creates" }'
}

check() {
    local name=$1 want=$2 got=$3
    if [ "$got" = "$want" ]; then
        printf 'ok   %s\n' "$name"
    else
        printf 'FAIL %s: want %q, got %q\n' "$name" "$want" "$got"
        FAILURES=$((FAILURES + 1))
    fi
}

test_every_migrated_table_has_a_line() {
    migrate_fresh_database
    check "every public table has a manifest line and every line a table" "" "$(manifest_gaps)"
}

test_unlisted_table_is_named() {
    migrate_fresh_database
    sql fresh -c "CREATE TABLE brand_new_table (id int)"
    check "a migrated table with no line is named" "public.brand_new_table has no line in staging-sync.tables" "$(manifest_gaps)"
}

test_stale_line_is_named() {
    migrate_fresh_database
    sql fresh -c "DROP TABLE acquisition_outcomes"
    check "a line with no table is named" "staging-sync.tables line names acquisition_outcomes, which no migration creates" "$(manifest_gaps)"
}

[ -f "$MANIFEST" ] || { printf 'FAIL: %s not found\n' "$MANIFEST"; exit 1; }
start_postgres
test_every_migrated_table_has_a_line
test_unlisted_table_is_named
test_stale_line_is_named

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi
printf '\nall staging-sync manifest checks passed\n'
