set -euo pipefail

COMPOSE_FILE=deploy/compose.prod.yml
UPSTREAM_FILE=deploy/caddy/upstream.conf
LEGACY_UPSTREAM_FILE=caddy/upstream.conf
PUBLIC_HEALTH_URL="${PUBLIC_HEALTH_URL:-https://altune.duckdns.org/health}"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-180}"
DRAIN_SECONDS="${DRAIN_SECONDS:-20}"

# Log signatures that mean the operator-token persistence or seed is broken — the
# #1471 prod bug: the token file can't be written, or a spent seed is being
# replayed. A generic overseer.collect.failed (e.g. the known OCI-usage 404, #1487)
# is deliberately absent, so only token/persist breakage fails the deploy or gate.
# Shared by overseer.sh (post-deploy smoke) and smoke.sh (promotion gate).
# shellcheck disable=SC2034  # consumed by the scripts that source this lib
TOKEN_FAILURE_SIGNATURES='permission denied|persisting rotated refresh token failed|refresh_token_already_used|read-only token refresh failed at status: status 400|read-only token refresh failed at password_grant'

compose() {
    docker compose -f "$COMPOSE_FILE" "$@"
}

log() {
    printf '[deploy] %s\n' "$*" >&2
}

write_upstream() {
    mkdir -p "$(dirname "$UPSTREAM_FILE")"
    printf 'reverse_proxy altune-go-api-%s:8000\n' "$1" >"$UPSTREAM_FILE"
}

seed_upstream_if_missing() {
    if [ -f "$UPSTREAM_FILE" ]; then
        return 0
    fi
    mkdir -p "$(dirname "$UPSTREAM_FILE")"
    if [ -f "$LEGACY_UPSTREAM_FILE" ]; then
        log "adopting the pre-move upstream file at $LEGACY_UPSTREAM_FILE"
        mv "$LEGACY_UPSTREAM_FILE" "$UPSTREAM_FILE"
        return 0
    fi
    if docker ps --format '{{.Names}}' | grep -qx altune-go-api; then
        log "first blue-green deploy: holding Caddy on the legacy container during the swap"
        printf 'reverse_proxy altune-go-api:8000\n' >"$UPSTREAM_FILE"
    else
        write_upstream blue
    fi
}

active_color() {
    seed_upstream_if_missing
    local color
    color=$(sed -n 's/.*altune-go-api-\(blue\|green\):8000.*/\1/p' "$UPSTREAM_FILE" | head -1)
    printf '%s' "${color:-blue}"
}

idle_color() {
    if [ "$1" = blue ]; then echo green; else echo blue; fi
}

reload_caddy() {
    compose exec -T caddy caddy reload --config /etc/caddy/Caddyfile
}

wait_healthy() {
    local color=$1
    local deadline=$((SECONDS + HEALTH_TIMEOUT))
    while [ "$SECONDS" -lt "$deadline" ]; do
        if compose exec -T caddy wget -q -O /dev/null "http://altune-go-api-$color:8000/health"; then
            return 0
        fi
        sleep 3
    done
    return 1
}

verify_public() {
    curl -fsS -o /dev/null --max-time 10 --retry 5 --retry-delay 3 "$PUBLIC_HEALTH_URL"
}

flip_to() {
    write_upstream "$1"
    reload_caddy
}

# --- shared migration runner (staging.sh + prod-migrate.sh) -------------------
# Both tiers apply migrations to their Supabase project through this ONE runner so
# the non-idempotent-safe logic can never drift between them (the lockstep
# invariant, #1525). Callers set ENV_FILE + MIGRATIONS_DIR, grep DATABASE_URL into
# MIGRATE_DATABASE_URL, then call apply_migrations <tier>. The URL is passed to
# psql as an argument only, never through log().

read_env_var() {
    # .env values can hold unquoted parens, so grep the line, don't `source` it.
    grep -E "^[[:space:]]*$1=" "$ENV_FILE" | head -1 | sed -E "s/^[[:space:]]*$1=//"
}

migration_versions() {
    local file
    for file in "$MIGRATIONS_DIR"/*.sql; do
        basename "$file" .sql
    done | sort -V
}

psql_value() {
    psql "$MIGRATE_DATABASE_URL" -tA -v ON_ERROR_STOP=1 -c "$1"
}

ensure_migration_tracker() {
    psql_value 'CREATE TABLE IF NOT EXISTS schema_migrations (
        version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now());' >/dev/null
}

tracked_migration_count() {
    psql_value 'SELECT count(*) FROM schema_migrations;'
}

schema_is_present() {
    [ "$(psql_value "SELECT to_regclass('public.tracks') IS NOT NULL;")" = t ]
}

# An already-migrated DB predates this tracker (migrations were applied by hand):
# the baseline table exists but nothing is tracked. Adopt the current set as the
# baseline so already-applied, non-idempotent migrations (016's bare ADD
# CONSTRAINT) are never re-run. ONLY sound when the DB is known to hold the full
# set (staging's documented lockstep invariant) — prod is NOT, so prod-migrate.sh
# gates this behind an explicit baseline check rather than calling it blindly.
adopt_existing_schema() {
    [ "$(tracked_migration_count)" = 0 ] || return 0
    schema_is_present || return 0
    log "adopting existing $MIGRATE_TIER schema as the migration baseline"
    local version
    for version in $(migration_versions); do
        psql_value "INSERT INTO schema_migrations (version) VALUES ('$version') ON CONFLICT DO NOTHING;" >/dev/null
    done
}

# CREATE INDEX CONCURRENTLY (and REINDEX/DROP INDEX CONCURRENTLY) are rejected by
# Postgres inside a transaction block, so such a file cannot be wrapped. The
# `-- migrate:no-transaction` header is the contract; the CONCURRENTLY scan, with
# comments stripped so a file that only discusses it in prose keeps its
# transaction, is the net under a migration whose author forgot the header.
migration_forbids_transaction() {
    local statements
    statements=$(sed -E 's/--.*//' "$1")
    grep -Eqi '^[[:space:]]*--[[:space:]]*migrate:no-transaction[[:space:]]*$' "$1" ||
        grep -qiw CONCURRENTLY <<<"$statements"
}

# --single-transaction: a half-applied migration rolls back rather than leaving
# the DB in a shape the tracker would then call applied. A no-transaction file
# gives that up, so its tracker INSERT is a separate autocommit statement that
# ON_ERROR_STOP keeps psql from reaching once the file has errored: a half-built
# CONCURRENTLY index is never recorded as applied, and the next run retries the
# file (drop the INVALID index first — see the header of migrations/020).
apply_migration_file() {
    local version=$1 file="$MIGRATIONS_DIR/$1.sql"
    local psql_args=("$MIGRATE_DATABASE_URL" -v ON_ERROR_STOP=1)
    if migration_forbids_transaction "$file"; then
        log "$version is no-transaction: applying it in autocommit"
    else
        psql_args+=(--single-transaction)
    fi
    psql "${psql_args[@]}" -f "$file" \
        -c "INSERT INTO schema_migrations (version) VALUES ('$version');"
}

apply_new_migrations() {
    local version
    for version in $(migration_versions); do
        if [ "$(psql_value "SELECT 1 FROM schema_migrations WHERE version='$version';")" = 1 ]; then
            continue
        fi
        log "applying $MIGRATE_TIER migration $version"
        apply_migration_file "$version"
    done
}

require_psql() {
    command -v psql >/dev/null || { log "FAILED: psql not found on PATH"; exit 1; }
}

# staging entrypoint: adopt-then-apply. Safe on staging because its schema is the
# documented lockstep baseline, so adopting the full set as applied is always true.
# prod-migrate.sh does NOT call this — prod's baseline is not guaranteed, so it
# gates adoption itself.
apply_migrations() {
    MIGRATE_TIER=$1
    require_psql
    ensure_migration_tracker
    adopt_existing_schema
    apply_new_migrations
}

capture_upstream() {
    PREVIOUS_UPSTREAM=$(cat "$UPSTREAM_FILE")
}

restore_upstream() {
    printf '%s\n' "$PREVIOUS_UPSTREAM" >"$UPSTREAM_FILE"
    reload_caddy
}
