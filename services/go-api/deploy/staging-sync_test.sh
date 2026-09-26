#!/usr/bin/env bash

# Self-test for staging-sync.sh against real Postgres: one throwaway container
# holds a "prod" and a "staging" database, both migrated from migrations/*.sql with
# a stand-in auth.users, and the script runs inside the container so the test needs
# no host psql. Asserts: matched accounts' rows land in staging under their staging
# UUID, while an unmatched prod account's rows never cross; staging-only accounts
# and their rows survive; orphaned_audio is never copied; a column only one tier
# has is skipped; prod is left byte-for-byte unchanged; a staging failure mid-swap
# rolls back to the old staging data; and the env gates refuse to run before
# touching anything, including when staging points at the prod database.

set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
FAILURES=0
RUN_ID="staging-sync-test-$$"
IMAGE=postgres:17
WORK=$(mktemp -d)

P1=11111111-1111-1111-1111-111111111111   # prod operator, has a staging twin
P2=22222222-2222-2222-2222-222222222222   # prod-only account, must never cross
S1=aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa   # staging twin of P1
S2=bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb   # staging-only smoke account
T1=10000000-0000-0000-0000-000000000001
T2=20000000-0000-0000-0000-000000000002
T_STALE=30000000-0000-0000-0000-000000000003
T_SMOKE=40000000-0000-0000-0000-000000000004
PL1=50000000-0000-0000-0000-000000000005
FA1=60000000-0000-0000-0000-000000000006

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
    cp "$HERE/lib.sh" "$HERE/staging-sync.sh" "$WORK/api/deploy/"
    cp -r "$HERE/../migrations" "$WORK/api/migrations"
    docker run -d --name "$RUN_ID" -e POSTGRES_PASSWORD=pw \
        -v "$WORK/api:/api" "$IMAGE" >/dev/null || exit 1
    local attempt
    for attempt in $(seq 1 60); do
        docker exec "$RUN_ID" pg_isready -q -U postgres -h 127.0.0.1 && return 0
        sleep 0.5
    done
    printf 'FAIL: postgres never became ready (%s tries)\n' "$attempt"
    exit 1
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
ALTER TABLE tracks ADD COLUMN prod_only text DEFAULT 'prod';
INSERT INTO auth.users VALUES ('$P1', 'Op@Example.com'), ('$P2', 'other@example.com');
INSERT INTO tracks (id, user_id, title, artist, dedup_key, audio_ref) VALUES
    ('$T1', '$P1', 'Mine', 'A', 'k1', 'a/mine.mp3'),
    ('$T2', '$P2', 'Theirs', 'B', 'k2', 'b/theirs.mp3');
INSERT INTO playlists (id, user_id, name) VALUES ('$PL1', '$P1', 'Main');
INSERT INTO playlist_tracks VALUES ('$PL1', '$T1', 0);
INSERT INTO featured_artists (id, user_id, name, norm_name) VALUES ('$FA1', '$P1', 'F', 'f');
INSERT INTO track_featured_artists VALUES ('$T1', '$FA1', 0);
INSERT INTO acquisition_cooldowns VALUES ('$T1', 'retry', now());
INSERT INTO discovery_search_history (id, user_id, query, query_norm) VALUES
    (gen_random_uuid(), '$P1', 'q', 'q'), (gen_random_uuid(), '$P2', 'secret', 'secret');
INSERT INTO playback_queue_state (user_id, track_ids, current_idx, position_ms, shuffled, repeat_mode)
    VALUES ('$P1', ARRAY['$T1'], 0, 0, false, 'off');
INSERT INTO entity_identity (provider, external_id, kind, mbid) VALUES ('deezer', '1', 'artist', 'mb-1');
INSERT INTO orphaned_audio (audio_ref, user_id, track_id) VALUES ('a/gone.mp3', '$P1', '$T1');
SQL
    sql staging <<SQL
ALTER TABLE tracks ADD COLUMN staging_only text;
INSERT INTO auth.users VALUES ('$S1', 'op@example.com'), ('$S2', 'smoke@example.com');
INSERT INTO tracks (id, user_id, title, artist, dedup_key) VALUES
    ('$T_STALE', '$S1', 'Stale', 'S', 'ks'),
    ('$T_SMOKE', '$S2', 'Smoke', 'S', 'km');
INSERT INTO entity_identity (provider, external_id, kind, mbid) VALUES ('deezer', 'stale', 'artist', 'mb-stale');
SQL
}

write_env() {
    local prod_db=${1:-prod} staging_db=${2:-staging}
    printf 'DATABASE_URL=postgresql://postgres:pw@127.0.0.1:5432/%s\nSUPABASE_PROJECT_URL=https://prod.supabase.co\n' \
        "$prod_db" >"$WORK/api/.env.production"
    printf 'DATABASE_URL=postgresql://postgres:pw@127.0.0.1:5432/%s\nSUPABASE_PROJECT_URL=https://staging.supabase.co\n' \
        "$staging_db" >"$WORK/api/.env.staging"
}

run_sync() {
    docker exec "$RUN_ID" bash /api/deploy/staging-sync.sh >"$WORK/out" 2>&1
}

# Prints 1 when the sync exits non-zero, 0 when it succeeds.
sync_refused() {
    if run_sync; then echo 0; else echo 1; fi
}

prod_fingerprint() {
    local table out=""
    for table in tracks playlists playlist_tracks featured_artists track_featured_artists \
        acquisition_cooldowns discovery_search_history playback_queue_state entity_identity orphaned_audio; do
        out="$out $table=$(sql prod -c "SELECT md5(coalesce(string_agg(t::text, '' ORDER BY t::text), '')) FROM $table t")"
    done
    printf '%s' "$out"
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

staging_value() {
    sql staging -c "$1"
}

test_sync_copies_matched_account() {
    reset_databases
    write_env
    local before
    before=$(prod_fingerprint)
    run_sync
    check "sync exits 0" 0 "$?"
    check "matched track lands under the staging UUID" "$S1" "$(staging_value "SELECT user_id FROM tracks WHERE id = '$T1'")"
    check "unmatched prod account's track never crosses" 0 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T2'")"
    check "no prod UUID left anywhere in staging" 0 "$(staging_value "SELECT count(*) FROM (SELECT user_id FROM tracks UNION ALL SELECT user_id FROM playlists UNION ALL SELECT user_id FROM discovery_search_history UNION ALL SELECT user_id FROM playback_queue_state UNION ALL SELECT user_id FROM featured_artists) u WHERE user_id IN ('$P1', '$P2')")"
    check "unmatched account's search history never crosses" 0 "$(staging_value "SELECT count(*) FROM discovery_search_history WHERE query = 'secret'")"
    check "matched account's stale staging track is replaced" 0 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T_STALE'")"
    check "staging-only account keeps its track" 1 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T_SMOKE'")"
    check "playlist tracks come across" 1 "$(staging_value "SELECT count(*) FROM playlist_tracks WHERE playlist_id = '$PL1' AND track_id = '$T1'")"
    check "featured artists come across" 1 "$(staging_value "SELECT count(*) FROM track_featured_artists WHERE track_id = '$T1'")"
    check "acquisition cooldowns come across" 1 "$(staging_value "SELECT count(*) FROM acquisition_cooldowns WHERE track_id = '$T1'")"
    check "playback queue lands under the staging UUID" 1 "$(staging_value "SELECT count(*) FROM playback_queue_state WHERE user_id = '$S1'")"
    check "entity cache is replaced by prod's" "mb-1" "$(staging_value "SELECT string_agg(mbid, ',') FROM entity_identity")"
    check "orphaned audio is never copied" 0 "$(staging_value "SELECT count(*) FROM orphaned_audio")"
    check "a staging-only column stays null" "" "$(staging_value "SELECT staging_only FROM tracks WHERE id = '$T1'")"
    check "the import schema is dropped" 0 "$(staging_value "SELECT count(*) FROM pg_namespace WHERE nspname = 'prod_import'")"
    check "prod is unchanged" "$before" "$(prod_fingerprint)"

    run_sync
    check "a second sync is idempotent" 1 "$(staging_value "SELECT count(*) FROM tracks WHERE user_id = '$S1'")"
}

test_failed_swap_rolls_back() {
    reset_databases
    write_env
    sql staging -c "ALTER TABLE tracks ADD COLUMN required_later text NOT NULL DEFAULT 'x'" \
        -c "ALTER TABLE tracks ALTER COLUMN required_later DROP DEFAULT"
    check "a failing swap exits non-zero" 1 "$(sync_refused)"
    check "the failure comes from the swap" 1 "$(grep -c 'null value in column "required_later"' "$WORK/out")"
    check "a failing swap keeps the old staging rows" 1 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T_STALE'")"
    check "a failing swap drops the import schema" 0 "$(staging_value "SELECT count(*) FROM pg_namespace WHERE nspname = 'prod_import'")"
}

test_env_gates() {
    reset_databases
    write_env prod prod
    check "staging pointing at prod is refused" 1 "$(sync_refused)"
    check "the refusal names the problem" 1 "$(grep -c 'points at the prod database' "$WORK/out")"

    write_env
    rm "$WORK/api/.env.staging"
    check "a missing staging env file is refused" 1 "$(sync_refused)"
    check "a refused run leaves staging untouched" 1 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T_STALE'")"

    write_env
    sql staging -c "DELETE FROM auth.users WHERE id = '$S1'"
    check "no matched account is refused" 1 "$(sync_refused)"
}

start_postgres
test_sync_copies_matched_account
test_failed_swap_rolls_back
test_env_gates

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed; last script output:\n' "$FAILURES"
    cat "$WORK/out"
    exit 1
fi
printf '\nall staging-sync checks passed\n'
