#!/usr/bin/env bash

set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
FAILURES=0

setup_case() {
    local seed=$1 health_ok=$2 public_ok=$3 legacy=$4 seed_at=${5:-deploy/caddy}
    WORK=$(mktemp -d)
    mkdir -p "$WORK/bin" "$WORK/api/deploy/caddy" "$WORK/api/caddy"
    cp "$HERE/lib.sh" "$HERE/blue-green.sh" "$WORK/api/deploy/"
    cp "$HERE/compose.prod.yml" "$WORK/api/deploy/"

    if [ -n "$seed" ]; then
        printf 'reverse_proxy altune-go-api-%s:8000\n' "$seed" >"$WORK/api/$seed_at/upstream.conf"
    fi

    cat >"$WORK/bin/docker" <<EOF
#!/usr/bin/env bash
echo "docker \$*" >> "$WORK/actions.log"
case "\$*" in
  "ps --format {{.Names}}") [ "$legacy" = yes ] && echo altune-go-api; exit 0 ;;
  *"wget"*"/health"*) exit $([ "$health_ok" = yes ] && echo 0 || echo 1) ;;
esac
exit 0
EOF
    cat >"$WORK/bin/curl" <<EOF
#!/usr/bin/env bash
exit $([ "$public_ok" = yes ] && echo 0 || echo 1)
EOF
    printf '#!/usr/bin/env bash\nexit 0\n' >"$WORK/bin/sleep"
    chmod +x "$WORK/bin"/*
    : >"$WORK/actions.log"

    (cd "$WORK/api" && PATH="$WORK/bin:$PATH" HEALTH_TIMEOUT=4 \
        bash deploy/blue-green.sh >"$WORK/out.log" 2>&1)
    RC=$?
    UPSTREAM=$(cat "$WORK/api/deploy/caddy/upstream.conf")
}

fail() {
    printf 'FAIL: %s\n      %s\n' "$CASE" "$1"
    FAILURES=$((FAILURES + 1))
}

expect_rc() {
    [ "$RC" = "$1" ] || fail "expected exit $1, got $RC"
}

expect_upstream() {
    [ "$UPSTREAM" = "$1" ] || fail "expected upstream '$1', got '$UPSTREAM'"
}

expect_action() {
    grep -qF "$1" "$WORK/actions.log" || fail "expected action '$1'"
}

expect_no_action() {
    grep -qF "$1" "$WORK/actions.log" && fail "unexpected action '$1'"
}

CASE="happy path swaps blue to green"
setup_case blue yes yes no
expect_rc 0
expect_upstream "reverse_proxy altune-go-api-green:8000"
expect_action "build go-api-green"
expect_action "stop go-api-blue"

CASE="happy path swaps green back to blue"
setup_case green yes yes no
expect_rc 0
expect_upstream "reverse_proxy altune-go-api-blue:8000"
expect_action "stop go-api-green"

CASE="an unhealthy candidate never touches production"
setup_case blue no yes no
expect_rc 1
expect_upstream "reverse_proxy altune-go-api-blue:8000"
expect_no_action "caddy reload"
expect_action "stop go-api-green"
expect_no_action "stop go-api-blue"

CASE="a failed public check rolls the upstream back"
setup_case blue yes no no
expect_rc 1
expect_upstream "reverse_proxy altune-go-api-blue:8000"
expect_action "stop go-api-green"
expect_no_action "stop go-api-blue"

CASE="first run holds the legacy container until the candidate is healthy"
setup_case "" yes yes yes
expect_rc 0
expect_upstream "reverse_proxy altune-go-api-green:8000"
expect_action "build go-api-green"

CASE="first run rolls back to the legacy container, not to a colour"
setup_case "" yes no yes
expect_rc 1
expect_upstream "reverse_proxy altune-go-api:8000"

CASE="a pre-move upstream file is adopted instead of reseeded to blue"
setup_case green yes yes no caddy
expect_rc 0
expect_upstream "reverse_proxy altune-go-api-blue:8000"
expect_action "build go-api-blue"
[ -f "$WORK/api/caddy/upstream.conf" ] && fail "legacy upstream file was left behind"

site_imports() {
    awk -v site="$1" '
        $0 == site " {" { inside = 1; next }
        inside && /^}/ { inside = 0 }
        inside && $1 == "import" { print $2 }
    ' "$HERE/Caddyfile"
}

mounted_host_path() {
    sed -n "s|^ *- \./\([^:]*\):$1:ro\$|\1|p" "$HERE/compose.prod.yml"
}

expect_internal_listener_serves() {
    local host_path
    host_path=$(mounted_host_path "$(site_imports "http://:8081")")
    [ -n "$host_path" ] || fail "the :8081 listener imports no file compose mounts into caddy"
    [ "$(cat "$WORK/api/deploy/$host_path" 2>/dev/null)" = "$1" ] ||
        fail "the :8081 listener's upstream is not '$1'"
}

CASE="a flip moves the internal overseer listener with the public site"
setup_case blue yes yes no
expect_rc 0
expect_internal_listener_serves "reverse_proxy altune-go-api-green:8000"

CASE="a rollback moves the internal overseer listener back with the public site"
setup_case blue yes no no
expect_rc 1
expect_internal_listener_serves "reverse_proxy altune-go-api-blue:8000"

CASE="each internal listener imports exactly its public site's upstream"
[ "$(site_imports "http://:8081")" = "$(site_imports altune.duckdns.org)" ] ||
    fail ":8081 does not import the prod site's upstream file"
[ "$(site_imports "http://:8082")" = "$(site_imports altune-staging.duckdns.org)" ] ||
    fail ":8082 does not import the staging site's upstream file"
[ -n "$(site_imports "http://:8081")" ] || fail "no :8081 internal listener in the Caddyfile"
[ -n "$(site_imports "http://:8082")" ] || fail "no :8082 internal listener in the Caddyfile"

CASE="the internal listeners are never published to the host"
grep -Eq '^ *- "?([0-9.]+:)?808[12]:' "$HERE/compose.prod.yml" &&
    fail "compose.prod.yml publishes an internal overseer listener port"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all blue-green deploy checks passed\n'
