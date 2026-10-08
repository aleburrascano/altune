#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
state=${E2E_STACK_DIR:-${TMPDIR:-/tmp}/altune-e2e-stack}
health_attempts=${E2E_HEALTH_ATTEMPTS:-60}
ready_attempts=${E2E_READY_ATTEMPTS:-60}
audio_bucket=altune-e2e
s3_key=e2e
s3_secret=e2e-secret
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

audio_bucket_ready() {
  python3 - "http://127.0.0.1:$(host_port_of s3 9000)" "$audio_bucket" "$s3_key" "$s3_secret" <<'PY'
import datetime, hashlib, hmac, sys, urllib.request
endpoint, bucket, key, secret = sys.argv[1:5]
host = endpoint.split("://", 1)[1]
now = datetime.datetime.now(datetime.timezone.utc)
amz_date, day = now.strftime("%Y%m%dT%H%M%SZ"), now.strftime("%Y%m%d")
payload = hashlib.sha256(b"").hexdigest()
headers = f"host:{host}\nx-amz-content-sha256:{payload}\nx-amz-date:{amz_date}\n"
signed = "host;x-amz-content-sha256;x-amz-date"
request = f"PUT\n/{bucket}\n\n{headers}\n{signed}\n{payload}"
scope = f"{day}/us-east-1/s3/aws4_request"
to_sign = f"AWS4-HMAC-SHA256\n{amz_date}\n{scope}\n{hashlib.sha256(request.encode()).hexdigest()}"
k = ("AWS4" + secret).encode()
for part in (day, "us-east-1", "s3", "aws4_request"):
    k = hmac.new(k, part.encode(), hashlib.sha256).digest()
signature = hmac.new(k, to_sign.encode(), hashlib.sha256).hexdigest()
req = urllib.request.Request(f"{endpoint}/{bucket}", method="PUT", headers={
    "x-amz-content-sha256": payload, "x-amz-date": amz_date,
    "Authorization": f"AWS4-HMAC-SHA256 Credential={key}/{scope}, SignedHeaders={signed}, Signature={signature}"})
try:
    urllib.request.urlopen(req, timeout=5)
except urllib.error.HTTPError as e:
    if b"BucketAlreadyOwnedByYou" not in e.read():
        sys.exit(1)
PY
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
    -p 127.0.0.1::5432 --tmpfs /var/lib/postgresql/data:rw,size=2g postgres:16-alpine -c fsync=off -c synchronous_commit=off -c full_page_writes=off
  start_container redis -p 127.0.0.1::6379 redis:7-alpine
  start_container s3 -p 127.0.0.1::9000 -e RUSTFS_ACCESS_KEY="$s3_key" -e RUSTFS_SECRET_KEY="$s3_secret" \
    --tmpfs /data:rw,size=1g,uid=10001 --tmpfs /logs:rw,uid=10001 rustfs/rustfs:1.0.1
  wait_until "$ready_attempts" postgres_ready
  wait_until "$ready_attempts" redis_ready
  wait_until "$ready_attempts" audio_bucket_ready
  apply_migrations
  database_url="postgres://altune:altune_dev@127.0.0.1:$(host_port_of postgres 5432)/altune?sslmode=disable"
  redis_url="redis://127.0.0.1:$(host_port_of redis 6379)"
  s3_endpoint="http://127.0.0.1:$(host_port_of s3 9000)"
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
    PROVIDER_REPLAY_ENABLED=true PROVIDER_REPLAY_DIR="$root/apps/mobile/e2e/fixtures/providers" \
    SPOTIFY_ENABLED=false SOUNDCLOUD_ENABLED=false APPLEMUSIC_ENABLED=false \
    AMAZONMUSIC_ENABLED=false YTMUSIC_ENABLED=false YTDLP_ENABLED=false \
    OCI_S3_ENDPOINT="$s3_endpoint" OCI_S3_ACCESS_KEY="$s3_key" OCI_S3_SECRET_KEY="$s3_secret" \
    OCI_S3_BUCKET="$audio_bucket" OCI_S3_REGION=us-east-1 \
    MUSIC_DIR="$state/music" DATABASE_URL="$database_url" REDIS_URL="$redis_url" \
    SUPABASE_PROJECT_URL="http://127.0.0.1:$jwks_port" \
    SUPABASE_JWT_JWKS_URL="http://127.0.0.1:$jwks_port/jwks.json" \
    CORS_ORIGINS="http://127.0.0.1:$web_port" \
    start_detached api "$state/api"
}

export_and_serve_web() {
  mkdir -p "$state/metro-tmp"
  (cd "$root/apps/mobile" && rm -rf dist \
    && TMPDIR="$state/metro-tmp" EXPO_PUBLIC_TEST_AUTH=1 EXPO_PUBLIC_API_URL="$api_url" \
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
