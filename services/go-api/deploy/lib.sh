set -euo pipefail

COMPOSE_FILE=deploy/compose.prod.yml
UPSTREAM_FILE=deploy/caddy/upstream.conf
LEGACY_UPSTREAM_FILE=caddy/upstream.conf
PUBLIC_HEALTH_URL=""
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-180}"
DRAIN_SECONDS="${DRAIN_SECONDS:-20}"

token_failures() {
    grep -E 'permission denied|persisting rotated refresh token failed|refresh_token_already_used|read-only token refresh failed at status: status 400|read-only token refresh failed at password_grant' || true
}

use_compose_file() {
    COMPOSE_FILE=$1
}

compose() {
    local git_sha
    git_sha=$(git rev-parse HEAD 2>/dev/null) || git_sha=unknown
    GIT_SHA="$git_sha" docker compose -f "$COMPOSE_FILE" "$@"
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

resolve_public_health_url() {
    local ENV_FILE=$1
    if [ ! -r "$ENV_FILE" ]; then
        log "FAILED: cannot read $ENV_FILE for PUBLIC_HEALTH_URL"
        exit 1
    fi
    PUBLIC_HEALTH_URL=$(read_env_var PUBLIC_HEALTH_URL || true)
    if [ -z "$PUBLIC_HEALTH_URL" ]; then
        log "FAILED: PUBLIC_HEALTH_URL is unset in $ENV_FILE"
        exit 1
    fi
    if ! [[ $PUBLIC_HEALTH_URL =~ ^https?://[^[:space:]]+$ ]]; then
        log "FAILED: PUBLIC_HEALTH_URL in $ENV_FILE is not an http(s) URL: '$PUBLIC_HEALTH_URL'"
        exit 1
    fi
}

verify_public() {
    curl -fsS -o /dev/null --max-time 10 --retry 5 --retry-delay 3 "$PUBLIC_HEALTH_URL"
}

flip_to() {
    write_upstream "$1"
    reload_caddy
}


read_env_var() {
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

adopt_existing_schema() {
    [ "$(tracked_migration_count)" = 0 ] || return 0
    schema_is_present || return 0
    log "adopting existing $MIGRATE_TIER schema as the migration baseline"
    local version
    for version in $(migration_versions); do
        psql_value "INSERT INTO schema_migrations (version) VALUES ('$version') ON CONFLICT DO NOTHING;" >/dev/null
    done
}

migration_forbids_transaction() {
    local statements
    statements=$(sed -E 's/--.*//' "$1")
    grep -Eqi '^[[:space:]]*--[[:space:]]*migrate:no-transaction[[:space:]]*$' "$1" ||
        grep -qiw CONCURRENTLY <<<"$statements"
}

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
