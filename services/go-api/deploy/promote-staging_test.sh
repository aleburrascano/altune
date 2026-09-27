#!/usr/bin/env bash

# Self-test for the `promote-staging` CLI command against real Postgres,
# alongside staging-sync_test.sh: one throwaway container holds a "prod" and a
# "staging" database, both migrated from migrations/*.sql with a stand-in
# auth.users, and a filesystem-backed audio store (MUSIC_DIR) stands in for the
# S3 bucket the two tiers actually share, so Copy is exercised for real without
# live OCI credentials. Asserts: a ready staging track lands in prod under the
# prod user id and a rewritten ref, a track whose (user_id, dedup_key) already
# exists in prod is skipped rather than duplicated or overwritten, and an
# unrelated pre-existing prod row is byte-for-byte unchanged after the run.

set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/.." && pwd)
FAILURES=0
RUN_ID="promote-staging-test-$$"
IMAGE=postgres:17
WORK=$(mktemp -d)

P1=11111111-1111-1111-1111-111111111111   # prod operator, has a staging twin
S1=aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa   # staging twin of P1

T_NEW=10000000-0000-0000-0000-000000000001    # staging-only, ready, promotable
T_DUP=20000000-0000-0000-0000-000000000002    # staging-only, ready, dedup already in prod
T_KEEP=30000000-0000-0000-0000-000000000003   # already in prod, must never change

cleanup() {
    docker rm -f "$RUN_ID" >/dev/null 2>&1
    rm -rf "$WORK"
}
trap cleanup EXIT

sql() {
    docker exec -i "$RUN_ID" psql -X -q -At -v ON_ERROR_STOP=1 -U postgres -d "$1" "${@:2}"
}

start_postgres() {
    mkdir -p "$WORK/api/deploy"
    cp -r "$HERE/../migrations" "$WORK/api/migrations"
    docker run -d --name "$RUN_ID" -e POSTGRES_PASSWORD=pw -p 127.0.0.1::5432 \
        -v "$WORK/api:/api" "$IMAGE" >/dev/null || exit 1
    local attempt
    for attempt in $(seq 1 60); do
        docker exec "$RUN_ID" pg_isready -q -U postgres -h 127.0.0.1 && break
        sleep 0.5
    done
    if [ "$attempt" -eq 60 ] && ! docker exec "$RUN_ID" pg_isready -q -U postgres -h 127.0.0.1; then
        printf 'FAIL: postgres never became ready\n'
        exit 1
    fi
    HOST_PORT=$(docker port "$RUN_ID" 5432/tcp | head -1 | cut -d: -f2)
    [ -n "$HOST_PORT" ] || { printf 'FAIL: could not read the published postgres port\n'; exit 1; }
}

reset_databases() {
    local db file
    for db in prod staging; do
        sql postgres -c "DROP DATABASE IF EXISTS $db" -c "CREATE DATABASE $db"
        sql "$db" -c "CREATE SCHEMA auth" -c "CREATE TABLE auth.users (id uuid PRIMARY KEY, email text)"
        for file in $(cd "$WORK/api" && printf '%s\n' migrations/*.sql | sort -V); do
            sql "$db" -f "/api/$file" >/dev/null || { printf 'FAIL: %s did not apply\n' "$file"; exit 1; }
        done
    done
    sql prod <<SQL
INSERT INTO auth.users VALUES ('$P1', 'Op@Example.com');
INSERT INTO tracks (id, user_id, title, artist, dedup_key, audio_ref, acquisition_status) VALUES
    ('$T_KEEP', '$P1', 'Already there', 'A', 'dk-existing', '$P1/A/Alb/kept.opus', 'ready');
SQL
    sql staging <<SQL
INSERT INTO auth.users VALUES ('$S1', 'op@example.com');
INSERT INTO tracks (id, user_id, title, artist, dedup_key, audio_ref, acquisition_status) VALUES
    ('$T_NEW', '$S1', 'New song', 'A', 'dk-new', 'staging/$S1/A/Alb/new.opus', 'ready'),
    ('$T_DUP', '$S1', 'Duplicate', 'A', 'dk-existing', 'staging/$S1/A/Alb/dup.opus', 'ready');
SQL
}

write_audio_fixtures() {
    rm -rf "$WORK/audio"
    mkdir -p "$WORK/audio/staging/$S1/A/Alb" "$WORK/audio/$P1/A/Alb"
    printf 'kept-bytes' >"$WORK/audio/$P1/A/Alb/kept.opus"
    printf 'new-bytes' >"$WORK/audio/staging/$S1/A/Alb/new.opus"
    printf 'dup-bytes' >"$WORK/audio/staging/$S1/A/Alb/dup.opus"
}

build_binary() {
    (cd "$ROOT" && go build -o "$WORK/app" ./cmd/api) || { printf 'FAIL: go build failed\n'; exit 1; }
}

run_promote() {
    local execute_flag=$1
    DATABASE_URL="postgresql://postgres:pw@127.0.0.1:${HOST_PORT}/prod?sslmode=disable" \
        STAGING_DATABASE_URL="postgresql://postgres:pw@127.0.0.1:${HOST_PORT}/staging?sslmode=disable" \
        MUSIC_DIR="$WORK/audio" \
        SUPABASE_PROJECT_URL="http://localhost" \
        SUPABASE_JWT_JWKS_URL="http://localhost/jwks" \
        "$WORK/app" promote-staging "$execute_flag" >"$WORK/out" 2>&1
}

prod_fingerprint() {
    sql prod -c "SELECT md5(coalesce(string_agg(t::text, '' ORDER BY t::text), '')) FROM tracks t WHERE id = '$T_KEEP'"
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

prod_value() {
    sql prod -c "$1"
}

test_promote_lands_a_new_row_and_skips_a_duplicate() {
    reset_databases
    write_audio_fixtures
    local before
    before=$(prod_fingerprint)

    run_promote --execute
    check "promote-staging exits 0" 0 "$?"
    check "the new track lands in prod under the prod user id" "$P1" "$(prod_value "SELECT user_id FROM tracks WHERE id = '$T_NEW'")"
    check "the new track's ref is rewritten to the prod user's prefix" "$P1/A/Alb/new.opus" \
        "$(prod_value "SELECT audio_ref FROM tracks WHERE id = '$T_NEW'")"
    check "the promoted audio bytes are copied onto the prod ref" "new-bytes" \
        "$(cat "$WORK/audio/$P1/A/Alb/new.opus" 2>/dev/null || echo MISSING)"
    check "the duplicate dedup_key track is skipped, not inserted" 0 \
        "$(prod_value "SELECT count(*) FROM tracks WHERE id = '$T_DUP'")"
    check "the skipped track's staging object is left in place" "dup-bytes" \
        "$(cat "$WORK/audio/staging/$S1/A/Alb/dup.opus" 2>/dev/null || echo MISSING)"
    check "the pre-existing prod row is unchanged" "$before" "$(prod_fingerprint)"
    check "prod still has exactly the original plus the one promoted row" 2 \
        "$(prod_value "SELECT count(*) FROM tracks")"

    run_promote --execute
    check "a second run is idempotent: no duplicate promotion" 1 \
        "$(prod_value "SELECT count(*) FROM tracks WHERE id = '$T_NEW'")"
}

test_dry_run_promotes_nothing() {
    reset_databases
    write_audio_fixtures

    run_promote ""
    check "a dry run exits 0" 0 "$?"
    check "a dry run inserts no prod row" 0 "$(prod_value "SELECT count(*) FROM tracks WHERE id = '$T_NEW'")"
    check "a dry run copies no object" MISSING "$(cat "$WORK/audio/$P1/A/Alb/new.opus" 2>/dev/null || echo MISSING)"
}

start_postgres
build_binary
test_promote_lands_a_new_row_and_skips_a_duplicate
test_dry_run_promotes_nothing

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed; last script output:\n' "$FAILURES"
    cat "$WORK/out"
    exit 1
fi
printf '\nall promote-staging checks passed\n'
