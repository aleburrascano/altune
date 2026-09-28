#!/usr/bin/env bash
set -euo pipefail
cid=$(docker run -d --rm -e POSTGRES_USER=altune -e POSTGRES_PASSWORD=altune_dev -e POSTGRES_DB=altune -p 127.0.0.1::5432 postgres:16-alpine)
drop_on_failure() {
  local rc=$?
  [ "$rc" -eq 0 ] || docker rm -f "$cid" >/dev/null 2>&1
  exit "$rc"
}
trap drop_on_failure EXIT
[ -n "${GITHUB_ENV:-}" ] && echo "PG_CID=$cid" >>"$GITHUB_ENV"
port=$(docker port "$cid" 5432/tcp | head -1 | awk -F: '{print $NF}')
url="postgres://altune:altune_dev@127.0.0.1:$port/altune?sslmode=disable"
for _ in $(seq 60); do
  pg_isready -q -d "$url" && break
  sleep 1
done
pg_isready -q -d "$url"
[ -n "${GITHUB_ENV:-}" ] && echo "DATABASE_URL=$url" >>"$GITHUB_ENV"
echo "PG_CID=$cid"
echo "DATABASE_URL=$url"
