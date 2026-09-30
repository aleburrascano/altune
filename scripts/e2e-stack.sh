#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
state=${E2E_STACK_DIR:-${TMPDIR:-/tmp}/altune-e2e-stack}
health_attempts=${E2E_HEALTH_ATTEMPTS:-60}
ready_attempts=${E2E_READY_ATTEMPTS:-60}
stub_rsa_modulus=0vx7agoebGcQSuuPiLJXZptN9nndrQmbXEps2aiAFbWhM78LhWx4cbbfAAtVT86zwu1RK7aPFFxuhDR1L6tSoc_BJECPebWKRXjBZCiFV4n3oknjhMstn64tZ_2W-5JsGY4Hc5n9yBXArwl93lqt7_RN5w6Cf0h4QyQ5v-65YGjQR0_FDW2QvzqY368QQMicAtaSqzs8KJZgnYb9c7d0zgdAZHzu6qMQvRL5hajrn1n91CbOpbISD08qNLyrdkt-bFTWhAI4vMQFh6WeZu0fM4lFd2NcRwr3XPksINHaQ-G_xBniIqbw0Ls1jF44-csFCur-kEgU8awapJzKnqDKgw

usage() {
  echo "usage: e2e-stack.sh up|down" >&2
  exit 2
}

free_port() {
  python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])'
}

wait_until() {
  local attempts=$1; shift
  local _
  for _ in $(seq "$attempts"); do
    "$@" && return 0
    sleep 1
  done
  "$@"
}

publish() {
  echo "$1=$2"
  [ -z "${GITHUB_ENV:-}" ] || echo "$1=$2" >>"$GITHUB_ENV"
}

start_detached() {
  local name=$1; shift
  "$@" >"$state/$name.log" 2>&1 &
  echo $! >"$state/$name.pid"
}

start_container() {
  local name=$1; shift
  docker run -d --rm --label altune-ci=1 "$@" >"$state/$name.cid"
}

host_port_of() {
  docker port "$(cat "$state/$1.cid")" "$2/tcp" | head -1 | awk -F: '{print $NF}'
}

postgres_ready() {
  docker exec "$(cat "$state/postgres.cid")" pg_isready -q -h 127.0.0.1 -p 5432 -U altune -d altune
}

redis_ready() {
  docker exec "$(cat "$state/redis.cid")" redis-cli ping >/dev/null
}

apply_migrations() {
  local f
  for f in "$root"/services/go-api/migrations/*.sql; do
    echo "applying $(basename "$f")"
    docker exec -i "$(cat "$state/postgres.cid")" psql -U altune -d altune -v ON_ERROR_STOP=1 -q <"$f"
  done
}

api_healthy() {
  curl -fs -o /dev/null "$1/health"
}

web_serving() {
  curl -fs -o /dev/null "$1/"
}

bring_up_services() {
  start_container postgres -e POSTGRES_USER=altune -e POSTGRES_PASSWORD=altune_dev -e POSTGRES_DB=altune \
    -p 127.0.0.1::5432 postgres:16-alpine
  start_container redis -p 127.0.0.1::6379 redis:7-alpine
  wait_until "$ready_attempts" postgres_ready
  wait_until "$ready_attempts" redis_ready
  apply_migrations
  database_url="postgres://altune:altune_dev@127.0.0.1:$(host_port_of postgres 5432)/altune?sslmode=disable"
  redis_url="redis://127.0.0.1:$(host_port_of redis 6379)"
}

start_jwks_stub() {
  mkdir -p "$state/jwks"
  echo "{\"keys\":[{\"kty\":\"RSA\",\"kid\":\"e2e-stub\",\"alg\":\"RS256\",\"use\":\"sig\",\"e\":\"AQAB\",\"n\":\"$stub_rsa_modulus\"}]}" >"$state/jwks/jwks.json"
  jwks_port=$(free_port)
  start_detached jwks python3 -m http.server "$jwks_port" --bind 127.0.0.1 --directory "$state/jwks"
}

start_api() {
  (cd "$root/services/go-api" && go build -o "$state/api" ./cmd/api)
  mkdir -p "$state/music"
  api_port=$(free_port)
  api_url="http://127.0.0.1:$api_port"
  ENV=test TEST_AUTH_ENABLED=true HOST=127.0.0.1 PORT="$api_port" \
    MUSIC_DIR="$state/music" DATABASE_URL="$database_url" REDIS_URL="$redis_url" \
    SUPABASE_PROJECT_URL="http://127.0.0.1:$jwks_port" \
    SUPABASE_JWT_JWKS_URL="http://127.0.0.1:$jwks_port/jwks.json" \
    CORS_ORIGINS="http://127.0.0.1:$web_port" \
    start_detached api "$state/api"
}

export_and_serve_web() {
  (cd "$root/apps/mobile" && rm -rf dist \
    && EXPO_PUBLIC_TEST_AUTH=1 EXPO_PUBLIC_API_URL="$api_url" \
      EXPO_PUBLIC_SUPABASE_URL=http://127.0.0.1:1 EXPO_PUBLIC_SUPABASE_ANON_KEY=e2e-anon-key \
      npx expo export -p web --dev)
  start_detached web python3 -m http.server "$web_port" --bind 127.0.0.1 --directory "$root/apps/mobile/dist"
}

up() {
  [ ! -e "$state" ] || { echo "e2e-stack: already up ($state exists), run down first" >&2; exit 1; }
  mkdir -p "$state"
  trap 'rc=$?; [ "$rc" -eq 0 ] || { tail -n 50 "$state/api.log" 2>/dev/null >&2 || true; down; }; exit "$rc"' EXIT
  bring_up_services
  start_jwks_stub
  web_port=$(free_port)
  start_api
  export_and_serve_web
  wait_until "$health_attempts" api_healthy "$api_url"
  wait_until "$health_attempts" web_serving "http://127.0.0.1:$web_port"
  publish API_URL "$api_url"
  publish WEB_URL "http://127.0.0.1:$web_port"
  publish DATABASE_URL "$database_url"
}

stop_pid_file() {
  local pid
  pid=$(cat "$1" 2>/dev/null) || return 0
  kill "$pid" 2>/dev/null || true
}

remove_container_file() {
  local cid
  cid=$(cat "$1" 2>/dev/null) || return 0
  docker rm -fv "$cid" >/dev/null 2>&1 || true
}

down() {
  [ -d "$state" ] || return 0
  local f
  for f in "$state"/*.pid; do [ -e "$f" ] && stop_pid_file "$f"; done
  for f in "$state"/*.cid; do [ -e "$f" ] && remove_container_file "$f"; done
  rm -rf "$state"
}

[ $# -eq 1 ] || usage
case "$1" in
  up) up ;;
  down) down ;;
  *) usage ;;
esac
