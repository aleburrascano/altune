set -euo pipefail

COMPOSE_FILE=docker-compose.prod.yml
UPSTREAM_FILE=caddy/upstream.conf
PUBLIC_HEALTH_URL="${PUBLIC_HEALTH_URL:-https://altune.duckdns.org/health}"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-180}"
DRAIN_SECONDS="${DRAIN_SECONDS:-20}"

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

capture_upstream() {
    PREVIOUS_UPSTREAM=$(cat "$UPSTREAM_FILE")
}

restore_upstream() {
    printf '%s\n' "$PREVIOUS_UPSTREAM" >"$UPSTREAM_FILE"
    reload_caddy
}
