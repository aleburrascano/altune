#!/usr/bin/env bash

set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
FAILURES=0
SKIPS=0
RUN_ID="caddy-routing-$$"
STAGING=altune-staging.duckdns.org
PROD=altune.duckdns.org
API_PATHS="/v1/library /v1/ /health /test/reset /observe/health"
WORK=$(mktemp -d)

caddy_image() {
    sed -n '/^  caddy:/,/^  [a-z]/ s/^ *image: *//p' "$HERE/compose.prod.yml" | head -1
}

cleanup() {
    docker rm -f "$RUN_ID-edge" "$RUN_ID-stub" >/dev/null 2>&1
    docker network rm "$RUN_ID" >/dev/null 2>&1
    rm -rf "$WORK"
}
trap cleanup EXIT

write_stub_config() {
    cat >"$WORK/stub.Caddyfile" <<'EOF'
{
	admin off
}
:8000 {
	respond "go-api {method} {uri}" 200
}
:8090 {
	respond "overseer {method} {uri}" 200
}
EOF
    cat >"$WORK/edge.Caddyfile" <<'EOF'
{
	local_certs
	skip_install_trust
}
import /etc/caddy/Caddyfile.sites
EOF
    printf 'reverse_proxy altune-go-api-blue:8000\n' >"$WORK/upstream.conf"
}

start_containers() {
    local image
    image=$(caddy_image)
    [ -n "$image" ] || { printf 'FAIL: no caddy image in compose.prod.yml\n'; exit 1; }
    mkdir -p "$WORK/web"
    docker network create "$RUN_ID" >/dev/null || exit 1
    docker run -d --name "$RUN_ID-stub" --network "$RUN_ID" \
        --network-alias altune-go-api-blue --network-alias altune-staging-go-api-blue \
        --network-alias altune-overseer --network-alias altune-staging-overseer \
        -v "$WORK/stub.Caddyfile:/etc/caddy/Caddyfile:ro" "$image" >/dev/null || exit 1
    docker run -d --name "$RUN_ID-edge" --network "$RUN_ID" -p 127.0.0.1::443 \
        -v "$WORK/edge.Caddyfile:/etc/caddy/Caddyfile:ro" \
        -v "$HERE/Caddyfile:/etc/caddy/Caddyfile.sites:ro" \
        -v "$WORK/upstream.conf:/etc/caddy/upstream.conf:ro" \
        -v "$HERE/caddy/staging-upstream.conf:/etc/caddy/staging-upstream.conf:ro" \
        -v "$WORK/web:/srv/web:ro" "$image" >/dev/null || exit 1
    EDGE_PORT=$(docker port "$RUN_ID-edge" 443/tcp | head -1 | sed 's/.*://')
}

wait_for_edge() {
    local attempt
    for attempt in $(seq 1 40); do
        [ "$(fetch_status "$STAGING" /health)" != 000 ] && return 0
        sleep 0.5
    done
    printf 'FAIL: the edge Caddy never answered (%s tries)\n' "$attempt"
    docker logs "$RUN_ID-edge" 2>&1 | tail -20
    exit 1
}

fetch() {
    local host=$1 path=$2 method=${3:-GET}
    curl -sk --max-time 10 -X "$method" -D "$WORK/headers" \
        --resolve "$host:$EDGE_PORT:127.0.0.1" "https://$host:$EDGE_PORT$path"
}

fetch_status() {
    curl -sk --max-time 5 -o /dev/null -w '%{http_code}' \
        --resolve "$1:$EDGE_PORT:127.0.0.1" "https://$1:$EDGE_PORT$2"
}

header_of() {
    grep -i "^$1:" "$WORK/headers" | head -1 | sed 's/^[^:]*: *//' | tr -d '\r'
}

publish_export() {
    local release="$WORK/web/staging/releases/abc1234"
    rm -rf "$WORK/web/staging"
    mkdir -p "$release/library" "$release/_expo/static/js/web"
    printf '<html>web index</html>' >"$release/index.html"
    printf '<html>web sign-in</html>' >"$release/sign-in.html"
    printf '<html>web library</html>' >"$release/library/index.html"
    printf 'web bundle' >"$release/_expo/static/js/web/entry.js"
    ln -sfn releases/abc1234 "$WORK/web/staging/current"
}

web_dir_missing() {
    rm -rf "$WORK/web/staging"
}

web_dir_empty() {
    rm -rf "$WORK/web/staging"
    mkdir -p "$WORK/web/staging/releases"
}

web_current_dangling() {
    rm -rf "$WORK/web/staging"
    mkdir -p "$WORK/web/staging/releases"
    ln -sfn releases/deadbee "$WORK/web/staging/current"
}

fail() {
    printf 'FAIL: %s\n      %s\n' "$CASE" "$1"
    FAILURES=$((FAILURES + 1))
}

skip() {
    printf 'SKIP: %s (un-skipped by "%s")\n' "$1" "$2"
    SKIPS=$((SKIPS + 1))
}

expect_body() {
    local host=$1 path=$2 want=$3 method=${4:-GET} body
    body=$(fetch "$host" "$path" "$method")
    [ "$body" = "$want" ] || fail "$method $host$path answered '$body', expected '$want'"
}

expect_api_paths_reach_go_api() {
    local host=$1 path
    for path in $API_PATHS; do
        expect_body "$host" "$path" "go-api GET $path"
    done
    expect_body "$host" /v1/library "go-api POST /v1/library" POST
    expect_body "$host" /overseer/ "overseer GET /"
}

expect_today_routing_on_staging() {
    expect_api_paths_reach_go_api "$STAGING"
    expect_body "$STAGING" / "go-api GET /"
    expect_body "$STAGING" /sign-in "go-api GET /sign-in"
}

command -v docker >/dev/null 2>&1 || { printf 'FAIL: docker is required for the routing test\n'; exit 1; }
write_stub_config
start_containers
wait_for_edge

CASE="mh01: with the export published, go-api paths reach go-api and /overseer reaches Overseer"
publish_export
expect_api_paths_reach_go_api "$STAGING"
expect_api_paths_reach_go_api "$PROD"

CASE="the published export serves its files, clean URLs, and directory indexes"
publish_export
expect_body "$STAGING" / "<html>web index</html>"
expect_body "$STAGING" /sign-in "<html>web sign-in</html>"
expect_body "$STAGING" /library "<html>web library</html>"
expect_body "$STAGING" /_expo/static/js/web/entry.js "web bundle"

CASE="a path the export does not hold falls through to go-api"
publish_export
expect_body "$STAGING" /no-such-page "go-api GET /no-such-page"
expect_body "$STAGING" /sign-in.json "go-api GET /sign-in.json"

CASE="mh02: with the web dir missing, staging routes exactly as before the web tier"
web_dir_missing
expect_today_routing_on_staging

CASE="mh02: with the web dir empty, staging routes exactly as before the web tier"
web_dir_empty
expect_today_routing_on_staging

CASE="mh02: with current pointing at a deleted release, staging routes exactly as before"
web_current_dangling
expect_today_routing_on_staging

CASE="mh06: web HTML carries a CSP whose script-src allows no inline or eval"
publish_export
fetch "$STAGING" / >/dev/null
CSP=$(header_of Content-Security-Policy)
SCRIPT_SRC=$(printf '%s' "$CSP" | tr ';' '\n' | sed -n 's/^ *script-src//p')
[ -n "$SCRIPT_SRC" ] || fail "no script-src directive in CSP '$CSP'"
case "$SCRIPT_SRC" in
    *"'unsafe-inline'"* | *"'unsafe-eval'"*) fail "script-src allows inline or eval: '$SCRIPT_SRC'" ;;
esac
printf '%s' "$CSP" | grep -q "frame-ancestors 'none'" || fail "CSP lacks frame-ancestors 'none'"
[ "$(header_of X-Content-Type-Options)" = nosniff ] || fail "web response lacks X-Content-Type-Options: nosniff"
[ "$(header_of Referrer-Policy)" = strict-origin-when-cross-origin ] || fail "web response lacks the Referrer-Policy"

CASE="the web security headers stay off go-api responses"
publish_export
fetch "$STAGING" /health >/dev/null
[ -z "$(header_of Content-Security-Policy)" ] || fail "go-api /health gained the web CSP"

CASE="connect-src opens raw.githubusercontent.com only at the kill-switch file the app polls"
publish_export
fetch "$STAGING" / >/dev/null
KILL_SWITCH_URL=$(sed -n "s|^ *'\(https://raw\.githubusercontent\.com/[^']*\)';$|\1|p" \
    "$HERE/../../../apps/mobile/src/shared/killSwitch/killSwitchPoll.ts")
[ -n "$KILL_SWITCH_URL" ] || fail "could not read DEFAULT_KILL_SWITCH_URL from killSwitchPoll.ts"
CONNECT_SRC=$(header_of Content-Security-Policy | tr ';' '\n' | sed -n 's/^ *connect-src//p')
tr ' ' '\n' <<<"$CONNECT_SRC" | grep -qxF "$KILL_SWITCH_URL" || fail "connect-src '$CONNECT_SRC' lacks $KILL_SWITCH_URL"
tr ' ' '\n' <<<"$CONNECT_SRC" | grep '^https://raw\.githubusercontent\.com' | grep -vqxF "$KILL_SWITCH_URL" &&
    fail "connect-src '$CONNECT_SRC' opens raw.githubusercontent.com beyond the kill-switch file"

CASE="the internal :8082 listener still reaches staging go-api with the export published"
publish_export
INTERNAL=$(docker exec "$RUN_ID-edge" wget -qO- http://127.0.0.1:8082/)
[ "$INTERNAL" = "go-api GET /" ] || fail ":8082 answered '$INTERNAL'"

CASE="mh05: a hard reload on a dynamic deep link renders that screen"
publish_export
mkdir -p "$WORK/web/staging/releases/abc1234/library/playlist"
printf '<html>web playlist [id]</html>' >"$WORK/web/staging/releases/abc1234/library/playlist/[id].html"
expect_body "$STAGING" /library/playlist/11111111-1111-1111-1111-111111111111 "<html>web playlist [id]</html>"
expect_body "$STAGING" "/library/playlist/11111111-1111-1111-1111-111111111111?from=discover" "<html>web playlist [id]</html>"

CASE="mh05: an unknown path under the dynamic segment's parent still falls through to go-api"
publish_export
mkdir -p "$WORK/web/staging/releases/abc1234/library/playlist"
printf '<html>web playlist [id]</html>' >"$WORK/web/staging/releases/abc1234/library/playlist/[id].html"
expect_body "$STAGING" "/library/playlist/11111111-1111-1111-1111-111111111111/extra" "go-api GET /library/playlist/11111111-1111-1111-1111-111111111111/extra"

CASE="mh05: a dynamic deep link without a published [id].html still falls through to go-api"
publish_export
expect_body "$STAGING" /library/playlist/11111111-1111-1111-1111-111111111111 "go-api GET /library/playlist/11111111-1111-1111-1111-111111111111"

CASE="mh05: HTML responses carry no-cache, hashed bundles carry immutable"
publish_export
fetch "$STAGING" / >/dev/null
[ "$(header_of Cache-Control)" = no-cache ] || fail "GET / carried Cache-Control '$(header_of Cache-Control)', expected no-cache"
fetch "$STAGING" /library >/dev/null
[ "$(header_of Cache-Control)" = no-cache ] || fail "GET /library carried Cache-Control '$(header_of Cache-Control)', expected no-cache"
fetch "$STAGING" /_expo/static/js/web/entry.js >/dev/null
[ "$(header_of Cache-Control)" = "public, max-age=31536000, immutable" ] ||
    fail "GET /_expo/static/js/web/entry.js carried Cache-Control '$(header_of Cache-Control)', expected immutable"

skip "mh11: the Supabase redirect allow-list after the change is a superset of before" \
    "web sign-in redirects"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all caddy routing checks passed (%s skipped)\n' "$SKIPS"
