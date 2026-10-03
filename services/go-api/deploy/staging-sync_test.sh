#!/usr/bin/env bash


set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
FAILURES=0
RUN_ID="staging-sync-test-$$"
IMAGE=postgres:17
WORK=$(mktemp -d)

P1=11111111-1111-1111-1111-111111111111
P2=22222222-2222-2222-2222-222222222222
S1=aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa
S2=bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb
T1=10000000-0000-0000-0000-000000000001
T2=20000000-0000-0000-0000-000000000002
T_STALE=30000000-0000-0000-0000-000000000003
T_SMOKE=40000000-0000-0000-0000-000000000004
PL1=50000000-0000-0000-0000-000000000005
FA1=60000000-0000-0000-0000-000000000006

cleanup() {
    docker rm -fv "$RUN_ID" >/dev/null 2>&1
    rm -rf "$WORK"
}
trap cleanup EXIT

sql() {
    docker exec -i "$RUN_ID" psql -X -q -At -v ON_ERROR_STOP=1 -U postgres -d "$1" "${@:2}"
}

install_docker_stub() {
    cat >"$WORK/docker-stub" <<'STUB'
#!/usr/bin/env bash
case "$1" in
  ps)
    if printf '%s' "$*" | grep -q "altune-staging-go-api-"; then
        echo altune-staging-go-api-blue
    else
        echo altune-go-api-blue
    fi
    ;;
  exec)
    if [ -f /tmp/docker-exec-should-fail ] && printf '%s' "$*" | grep -q "promote-staging"; then
        echo "stubbed promote-staging failure" >&2
        exit 1
    fi
    exit 0
    ;;
  *)
    exit 0
    ;;
esac
STUB
    chmod +x "$WORK/docker-stub"
    docker cp "$WORK/docker-stub" "$RUN_ID:/usr/local/bin/docker"
}

start_postgres() {
    mkdir -p "$WORK/api/deploy"
    cp "$HERE/lib.sh" "$HERE/staging-sync.sh" "$HERE/staging-sync.tables" "$WORK/api/deploy/"
    cp -r "$HERE/../migrations" "$WORK/api/migrations"
    docker run -d --label altune-ci=1 --name "$RUN_ID" -e POSTGRES_PASSWORD=pw \
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
        sql "$db" -c "CREATE SCHEMA auth" -c "CREATE TABLE auth.users (id uuid PRIMARY KEY, email text)" \
            -c "CREATE TABLE schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())"
        local applied_versions=()
        for file in $(cd "$WORK/api" && printf '%s\n' migrations/*.sql | sort -V); do
            sql "$db" -f "/api/$file" >/dev/null || { printf 'FAIL: %s did not apply\n' "$file"; exit 1; }
            applied_versions+=("$(basename "$file" .sql)")
        done
        printf "INSERT INTO schema_migrations (version) VALUES ('%s');\n" "${applied_versions[@]}" | sql "$db"
    done
    sql prod <<SQL
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

sync_refused() {
    if run_sync; then echo 0; else echo 1; fi
}

manifest_tables() {
    awk '$1 != "" && $1 !~ /^#/ { print $1 }' "$HERE/staging-sync.tables"
}

prod_fingerprint() {
    local table out=""
    for table in $(manifest_tables); do
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
    check "the import schema is dropped" 0 "$(staging_value "SELECT count(*) FROM pg_namespace WHERE nspname = 'prod_import'")"
    check "prod is unchanged" "$before" "$(prod_fingerprint)"

    run_sync
    check "a second sync is idempotent" 1 "$(staging_value "SELECT count(*) FROM tracks WHERE user_id = '$S1'")"
}

test_failed_swap_rolls_back() {
    reset_databases
    write_env
    sql prod -c "ALTER TABLE tracks ADD COLUMN required_later text"
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

test_promote_failure_aborts_sync() {
    reset_databases
    write_env
    docker exec "$RUN_ID" touch /tmp/docker-exec-should-fail
    check "a promote-staging failure aborts the sync" 1 "$(sync_refused)"
    check "the failure names promote-staging" 1 "$(grep -c 'promote-staging failed' "$WORK/out")"
    check "an aborted sync leaves the old staging rows" 1 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T_STALE'")"
    docker exec "$RUN_ID" rm -f /tmp/docker-exec-should-fail
}

test_table_without_a_rule_aborts_sync() {
    reset_databases
    write_env
    sql prod -c "CREATE TABLE foo (id int)"
    check "a prod table with no rule aborts the sync" 1 "$(sync_refused)"
    check "the abort names the table" 1 "$(grep -c 'FAILED: public.foo has no rule in staging-sync.tables' "$WORK/out")"
    check "an unruled table leaves the old staging rows" 1 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T_STALE'")"
    check "an unruled table leaves staging without prod's track" 0 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T1'")"
}

restore_manifest() {
    cp "$HERE/staging-sync.tables" "$WORK/api/deploy/staging-sync.tables"
}

test_staging_only_table_without_a_rule_aborts_sync() {
    reset_databases
    write_env
    sql staging -c "CREATE TABLE bar (id int)"
    check "a staging-only table with no rule aborts the sync" 1 "$(sync_refused)"
    check "the abort names the staging-only table" 1 "$(grep -c 'FAILED: public.bar has no rule in staging-sync.tables' "$WORK/out")"
    check "a staging-only unruled table leaves the old staging rows" 1 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T_STALE'")"
    check "a staging-only unruled table leaves staging without prod's track" 0 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T1'")"
}

test_manifest_line_for_a_table_on_neither_tier_aborts_sync() {
    reset_databases
    write_env
    printf 'ghost_table global\n' >>"$WORK/api/deploy/staging-sync.tables"
    check "a manifest line naming no table aborts the sync" 1 "$(sync_refused)"
    check "the abort names the stale line's table" 1 "$(grep -c 'ghost_table' "$WORK/out")"
    check "a stale manifest line leaves the old staging rows" 1 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T_STALE'")"
    check "a stale manifest line leaves staging without prod's track" 0 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T1'")"
    restore_manifest
}

test_commented_out_line_is_not_a_rule() {
    reset_databases
    write_env
    printf '# foo user\n#foo global\n' >>"$WORK/api/deploy/staging-sync.tables"
    sql prod -c "CREATE TABLE foo (id int)"
    check "a table named only in a comment aborts the sync" 1 "$(sync_refused)"
    check "the abort names the commented-out table" 1 "$(grep -c 'FAILED: public.foo has no rule in staging-sync.tables' "$WORK/out")"
    check "a commented-out rule leaves the old staging rows" 1 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T_STALE'")"
    restore_manifest
}

test_manifest_with_comments_blank_lines_and_tabs_syncs() {
    reset_databases
    write_env
    {
        printf '# staging sync rules\n\n'
        sed 's/ /\t/' "$HERE/staging-sync.tables" | sed 's/^\(tracks\)\t/\1   /'
        printf '\n   \n# trailing comment\n'
    } >"$WORK/api/deploy/staging-sync.tables"
    run_sync
    check "a manifest with comments, blank lines and tabs syncs" 0 "$?"
    check "a reformatted manifest still re-keys the matched track" "$S1" "$(staging_value "SELECT user_id FROM tracks WHERE id = '$T1'")"
    check "a reformatted manifest still copies playlist tracks" 1 "$(staging_value "SELECT count(*) FROM playlist_tracks WHERE playlist_id = '$PL1' AND track_id = '$T1'")"
    restore_manifest
}

test_skip_tables_are_never_touched() {
    reset_databases
    write_env
    sql prod -c "INSERT INTO acquisition_outcomes (track_id, outcome, reason, elapsed_ms) VALUES ('$T1', 'succeeded', 'from-prod', 1)" \
        -c "INSERT INTO acquisition_rejections (track_id, source_key, reason) VALUES ('$T1', 'prod-src', 'from-prod')"
    sql staging -c "INSERT INTO acquisition_outcomes (track_id, outcome, reason, elapsed_ms) VALUES ('$T_STALE', 'failed', 'from-staging', 2)" \
        -c "INSERT INTO acquisition_rejections (track_id, source_key, reason) VALUES ('$T_STALE', 'staging-src', 'from-staging')"
    run_sync
    check "a sync with skip tables exits 0" 0 "$?"
    check "skipped outcomes keep only staging's rows" "from-staging" "$(staging_value "SELECT string_agg(reason, ',') FROM acquisition_outcomes")"
    check "skipped rejections keep only staging's rows" "from-staging" "$(staging_value "SELECT string_agg(reason, ',') FROM acquisition_rejections")"
}

test_reset_clears_only_matched_users_orphaned_audio() {
    reset_databases
    write_env
    sql staging -c "INSERT INTO orphaned_audio (audio_ref, user_id, track_id) VALUES ('s/matched.mp3', '$S1', '$T_STALE'), ('s/smoke.mp3', '$S2', '$T_SMOKE')"
    run_sync
    check "a sync with staging orphaned audio exits 0" 0 "$?"
    check "reset keeps only the unmatched account's orphaned audio" "s/smoke.mp3" "$(staging_value "SELECT string_agg(audio_ref, ',') FROM orphaned_audio")"
}

test_manifest_rule_for_a_table_on_one_tier_only_syncs() {
    reset_databases
    write_env
    sql staging -c "CREATE TABLE staging_scratch (id int)" -c "INSERT INTO staging_scratch VALUES (7)"
    printf 'staging_scratch skip staging-only scratch table\n' >>"$WORK/api/deploy/staging-sync.tables"
    run_sync
    check "a ruled table present on one tier only does not abort the sync" 0 "$?"
    check "a ruled one-tier table still lets the matched track cross" "$S1" "$(staging_value "SELECT user_id FROM tracks WHERE id = '$T1'")"
    check "a skipped one-tier table keeps its rows" 7 "$(staging_value "SELECT string_agg(id::text, ',') FROM staging_scratch")"
    restore_manifest
}

test_missing_manifest_aborts_sync() {
    reset_databases
    write_env
    rm "$WORK/api/deploy/staging-sync.tables"
    check "a missing manifest aborts the sync" 1 "$(sync_refused)"
    check "a missing manifest leaves the old staging rows" 1 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T_STALE'")"
    check "a missing manifest leaves staging without prod's track" 0 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T1'")"
    restore_manifest
}

test_table_with_two_rules_aborts_sync() {
    reset_databases
    write_env
    printf 'tracks global\n' >>"$WORK/api/deploy/staging-sync.tables"
    check "a table with two rules aborts the sync" 1 "$(sync_refused)"
    check "a doubly-ruled table leaves the old staging rows" 1 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T_STALE'")"
    check "a doubly-ruled table never copies the unmatched account's track" 0 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T2'")"
    restore_manifest
}

test_unknown_rule_aborts_sync() {
    reset_databases
    write_env
    sed 's/^entity_identity global$/entity_identity mirror/' "$HERE/staging-sync.tables" >"$WORK/api/deploy/staging-sync.tables"
    check "an unknown rule aborts the sync" 1 "$(sync_refused)"
    check "an unknown rule leaves the old staging rows" 1 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T_STALE'")"
    check "an unknown rule leaves the stale entity cache" "mb-stale" "$(staging_value "SELECT string_agg(mbid, ',') FROM entity_identity")"
    restore_manifest
}

test_prod_only_column_aborts_sync() {
    reset_databases
    write_env
    sql prod -c "ALTER TABLE tracks ADD COLUMN bar text"
    check "a prod-only column aborts the sync" 1 "$(sync_refused)"
    check "the abort names the drifted column" 1 "$(grep -c 'FAILED: tracks.bar differs: prod text, staging missing' "$WORK/out")"
    check "a prod-only column leaves the old staging rows" 1 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T_STALE'")"
    check "a prod-only column leaves staging without prod's track" 0 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T1'")"
}

test_staging_only_column_aborts_sync() {
    reset_databases
    write_env
    sql staging -c "ALTER TABLE tracks ADD COLUMN staging_only text"
    check "a staging-only column aborts the sync" 1 "$(sync_refused)"
    check "the abort names the staging-only column" 1 "$(grep -c 'FAILED: tracks.staging_only differs: prod missing, staging text' "$WORK/out")"
    check "a staging-only column leaves staging without prod's track" 0 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T1'")"
}

test_array_element_type_drift_aborts_sync() {
    reset_databases
    write_env
    sql prod -c "ALTER TABLE tracks ADD COLUMN tags text[]"
    sql staging -c "ALTER TABLE tracks ADD COLUMN tags integer[]"
    check "an array element type drift aborts the sync" 1 "$(sync_refused)"
    check "the abort names both array types" 1 "$(grep -c 'FAILED: tracks.tags differs: prod text\[\], staging integer\[\]' "$WORK/out")"
    check "an array type drift leaves staging without prod's track" 0 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T1'")"
}

test_generated_column_drift_aborts_sync() {
    reset_databases
    write_env
    sql prod -c "ALTER TABLE tracks ADD COLUMN title_upper text GENERATED ALWAYS AS (upper(title)) STORED"
    sql staging -c "ALTER TABLE tracks ADD COLUMN title_upper text"
    check "a column generated on one tier only aborts the sync" 1 "$(sync_refused)"
    check "the abort names the generated column" 1 "$(grep -c 'FAILED: tracks.title_upper differs: prod text generated, staging text' "$WORK/out")"
}

test_generated_column_on_both_tiers_syncs() {
    reset_databases
    write_env
    sql prod -c "ALTER TABLE tracks ADD COLUMN title_upper text GENERATED ALWAYS AS (upper(title)) STORED"
    sql staging -c "ALTER TABLE tracks ADD COLUMN title_upper text GENERATED ALWAYS AS (upper(title)) STORED"
    run_sync
    check "a column generated on both tiers syncs" 0 "$?"
    check "a generated column is recomputed on staging" "MINE" "$(staging_value "SELECT title_upper FROM tracks WHERE id = '$T1'")"
}

test_staging_ahead_with_a_migration_file_skips_sync() {
    reset_databases
    write_env
    touch "$WORK/api/migrations/033_x.sql"
    sql staging -c "INSERT INTO schema_migrations (version) VALUES ('033_x')"
    docker exec "$RUN_ID" touch /tmp/docker-exec-should-fail
    run_sync
    check "staging ahead of prod skips the sync with exit 0" 0 "$?"
    check "the skip names the pending version" 1 "$(grep -c 'SKIPPED: staging is ahead by 033_x$' "$WORK/out")"
    check "a skipped sync never runs promote-staging" 0 "$(grep -c 'promote-staging' "$WORK/out")"
    check "a skipped sync leaves the old staging rows" 1 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T_STALE'")"
    check "a skipped sync leaves staging without prod's track" 0 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T1'")"
    check "a skipped sync never creates the import schema" 0 "$(staging_value "SELECT count(*) FROM pg_namespace WHERE nspname = 'prod_import'")"
    docker exec "$RUN_ID" rm -f /tmp/docker-exec-should-fail
    rm "$WORK/api/migrations/033_x.sql"
}

test_staging_ahead_without_a_migration_file_aborts_sync() {
    reset_databases
    write_env
    sql staging -c "INSERT INTO schema_migrations (version) VALUES ('033_gone')"
    check "a staging version with no file aborts the sync" 1 "$(sync_refused)"
    check "the abort names the fileless version" 1 "$(grep -c 'FAILED: staging has migration 033_gone with no file' "$WORK/out")"
    check "a fileless staging version leaves staging without prod's track" 0 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T1'")"
}

test_staging_version_shaped_like_a_path_aborts_sync() {
    reset_databases
    write_env
    sql staging -c "INSERT INTO schema_migrations (version) VALUES ('../migrations/001_baseline')"
    check "a staging version shaped like a path aborts the sync" 1 "$(sync_refused)"
    check "the abort names the path-shaped version" 1 "$(grep -c 'FAILED: staging has migration ../migrations/001_baseline with no file' "$WORK/out")"
}

test_prod_ahead_of_staging_aborts_sync() {
    reset_databases
    write_env
    sql prod -c "INSERT INTO schema_migrations (version) VALUES ('033_prod_first')"
    check "a prod version staging lacks aborts the sync" 1 "$(sync_refused)"
    check "the abort names the version staging lacks" 1 "$(grep -c 'FAILED: prod has migration 033_prod_first that staging lacks' "$WORK/out")"
    check "prod ahead leaves staging without prod's track" 0 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T1'")"
}

test_prod_fingerprint_covers_every_manifest_table() {
    local before
    reset_databases
    before=$(prod_fingerprint)
    check "prod fingerprint names every manifest table" "$(manifest_tables | paste -sd' ')" \
        "$(printf '%s' "$before" | tr ' ' '\n' | sed -n 's/=.*//p' | paste -sd' ')"
}

test_staging_ahead_by_a_fileless_and_a_filed_version_aborts_sync() {
    reset_databases
    write_env
    touch "$WORK/api/migrations/033_x.sql"
    sql staging -c "INSERT INTO schema_migrations (version) VALUES ('033_x'), ('034_gone')"
    check "a fileless version among filed extras aborts the sync" 1 "$(sync_refused)"
    check "the abort names the fileless version among filed extras" 1 "$(grep -c 'FAILED: staging has migration 034_gone with no file' "$WORK/out")"
    check "a fileless version among filed extras is not reported as a skip" 0 "$(grep -c 'SKIPPED:' "$WORK/out")"
    check "a fileless version among filed extras leaves staging without prod's track" 0 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T1'")"
    rm "$WORK/api/migrations/033_x.sql"
}

test_staging_ahead_by_two_filed_versions_skips_naming_both() {
    reset_databases
    write_env
    touch "$WORK/api/migrations/033_x.sql" "$WORK/api/migrations/034_y.sql"
    sql staging -c "INSERT INTO schema_migrations (version) VALUES ('033_x'), ('034_y')"
    run_sync
    check "staging ahead by two filed versions exits 0" 0 "$?"
    check "the skip names both pending versions" 1 "$(grep -c 'SKIPPED: staging is ahead by 033_x 034_y$' "$WORK/out")"
    check "staging ahead by two versions leaves staging without prod's track" 0 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T1'")"
    rm "$WORK/api/migrations/033_x.sql" "$WORK/api/migrations/034_y.sql"
}

test_staging_ahead_mid_contract_skips_despite_column_drift() {
    reset_databases
    write_env
    touch "$WORK/api/migrations/033_contract_x.sql"
    sql staging -c "INSERT INTO schema_migrations (version) VALUES ('033_contract_x')" \
        -c "ALTER TABLE tracks ADD COLUMN mid_deploy text"
    run_sync
    check "staging ahead with drifted columns skips with exit 0" 0 "$?"
    check "a mid-deploy column drift is reported as a skip" 1 "$(grep -c 'SKIPPED: staging is ahead by 033_contract_x$' "$WORK/out")"
    check "a mid-deploy column drift is not reported as a failure" 0 "$(grep -c 'FAILED:' "$WORK/out")"
    check "a mid-deploy skip leaves the old staging rows" 1 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T_STALE'")"
    rm "$WORK/api/migrations/033_contract_x.sql"
}

test_prod_ahead_while_staging_ahead_aborts_sync() {
    reset_databases
    write_env
    touch "$WORK/api/migrations/033_x.sql"
    sql staging -c "INSERT INTO schema_migrations (version) VALUES ('033_x')"
    sql prod -c "INSERT INTO schema_migrations (version) VALUES ('033_prod_first')"
    check "prod ahead while staging is also ahead aborts the sync" 1 "$(sync_refused)"
    check "the abort names the prod version staging lacks, not a skip" 1 "$(grep -c 'FAILED: prod has migration 033_prod_first that staging lacks' "$WORK/out")"
    check "diverged tiers leave staging without prod's track" 0 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T1'")"
    rm "$WORK/api/migrations/033_x.sql"
}

test_column_drift_on_a_child_table_aborts_sync() {
    reset_databases
    write_env
    sql prod -c "ALTER TABLE playlist_tracks ADD COLUMN note text"
    check "a prod-only column on a child table aborts the sync" 1 "$(sync_refused)"
    check "the abort names the child table's column" 1 "$(grep -c 'FAILED: playlist_tracks.note differs: prod text, staging missing' "$WORK/out")"
    check "a child-table drift leaves staging without prod's track" 0 "$(staging_value "SELECT count(*) FROM tracks WHERE id = '$T1'")"
}

test_type_modifier_drift_on_a_global_table_aborts_sync() {
    reset_databases
    write_env
    sql prod -c "ALTER TABLE entity_identity ADD COLUMN label varchar(10)"
    sql staging -c "ALTER TABLE entity_identity ADD COLUMN label varchar(20)"
    check "a varchar length drift aborts the sync" 1 "$(sync_refused)"
    check "the abort names both varchar lengths" 1 "$(grep -c 'FAILED: entity_identity.label differs: prod character varying(10), staging character varying(20)' "$WORK/out")"
    check "a varchar length drift leaves the stale entity cache" "mb-stale" "$(staging_value "SELECT string_agg(mbid, ',') FROM entity_identity")"
}

test_column_drift_on_a_skip_table_still_syncs() {
    reset_databases
    write_env
    sql prod -c "ALTER TABLE acquisition_outcomes ADD COLUMN prod_extra text"
    run_sync
    check "column drift on a skip table does not abort the sync" 0 "$?"
    check "a skip-table drift still lets the matched track cross" "$S1" "$(staging_value "SELECT user_id FROM tracks WHERE id = '$T1'")"
}

test_a_dropped_column_on_one_tier_is_not_drift() {
    reset_databases
    write_env
    sql prod -c "ALTER TABLE tracks ADD COLUMN gone text" -c "ALTER TABLE tracks DROP COLUMN gone"
    run_sync
    check "a column dropped on prod and never on staging does not abort the sync" 0 "$?"
    check "a dropped-column tier still lets the matched track cross" "$S1" "$(staging_value "SELECT user_id FROM tracks WHERE id = '$T1'")"
}

test_same_columns_in_a_different_order_copy_by_name() {
    reset_databases
    write_env
    sql prod -c "ALTER TABLE tracks ADD COLUMN first_col text" -c "ALTER TABLE tracks ADD COLUMN second_col text" \
        -c "UPDATE tracks SET first_col = 'one', second_col = 'two' WHERE id = '$T1'"
    sql staging -c "ALTER TABLE tracks ADD COLUMN second_col text" -c "ALTER TABLE tracks ADD COLUMN first_col text"
    run_sync
    check "the same columns in a different order sync" 0 "$?"
    check "reordered columns land by name" "one,two" "$(staging_value "SELECT first_col || ',' || second_col FROM tracks WHERE id = '$T1'")"
}

start_postgres
install_docker_stub
test_sync_copies_matched_account
test_failed_swap_rolls_back
test_env_gates
test_promote_failure_aborts_sync
test_table_without_a_rule_aborts_sync
test_staging_only_table_without_a_rule_aborts_sync
test_manifest_line_for_a_table_on_neither_tier_aborts_sync
test_commented_out_line_is_not_a_rule
test_manifest_with_comments_blank_lines_and_tabs_syncs
test_skip_tables_are_never_touched
test_reset_clears_only_matched_users_orphaned_audio
test_manifest_rule_for_a_table_on_one_tier_only_syncs
test_missing_manifest_aborts_sync
test_table_with_two_rules_aborts_sync
test_unknown_rule_aborts_sync
test_prod_only_column_aborts_sync
test_staging_only_column_aborts_sync
test_array_element_type_drift_aborts_sync
test_generated_column_drift_aborts_sync
test_generated_column_on_both_tiers_syncs
test_staging_ahead_with_a_migration_file_skips_sync
test_staging_ahead_without_a_migration_file_aborts_sync
test_staging_version_shaped_like_a_path_aborts_sync
test_prod_ahead_of_staging_aborts_sync
test_prod_fingerprint_covers_every_manifest_table
test_staging_ahead_by_a_fileless_and_a_filed_version_aborts_sync
test_staging_ahead_by_two_filed_versions_skips_naming_both
test_staging_ahead_mid_contract_skips_despite_column_drift
test_prod_ahead_while_staging_ahead_aborts_sync
test_column_drift_on_a_child_table_aborts_sync
test_type_modifier_drift_on_a_global_table_aborts_sync
test_column_drift_on_a_skip_table_still_syncs
test_a_dropped_column_on_one_tier_is_not_drift
test_same_columns_in_a_different_order_copy_by_name

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed; last script output:\n' "$FAILURES"
    cat "$WORK/out"
    exit 1
fi
printf '\nall staging-sync checks passed\n'
