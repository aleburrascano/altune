#!/usr/bin/env bash

set -euo pipefail

cd "$(mktemp -d)"
fetch() { curl -sS --max-time 20 --retry 5 --retry-delay 3 --retry-all-errors "$@"; }
fetch -D index.headers -o index.html "$BASE_URL/"
grep -qE '^HTTP/[0-9.]+ 200' index.headers || { echo "::error::GET / is not 200"; cat index.headers; exit 1; }
grep -qi '^content-type: text/html' index.headers || { echo "::error::GET / is not HTML"; exit 1; }
csp=$(grep -i '^content-security-policy:' index.headers | tr -d '\r')
[ -n "$csp" ] || { echo "::error::GET / carries no Content-Security-Policy"; exit 1; }
script_src=$(printf '%s' "$csp" | tr ';' '\n' | grep -i 'script-src' || true)
case "$script_src" in
  *unsafe-inline* | *unsafe-eval* | "") echo "::error::script-src is missing or unsafe: $script_src"; exit 1 ;;
esac
grep -qF "$ENTRY" index.html || { echo "::error::GET / does not serve the release built from $SHA ($ENTRY)"; exit 1; }
fetch -o health.json "$BASE_URL/health"
jq -e '.status == "ok"' health.json >/dev/null || { echo "::error::/health is not go-api's JSON"; cat health.json; exit 1; }
overseer=$(fetch -o /dev/null -w '%{http_code}' "$BASE_URL/overseer/")
[ "$overseer" = 200 ] || { echo "::error::/overseer/ answered $overseer"; exit 1; }
echo "$BASE_URL serves $ENTRY with a strict CSP; /health and /overseer/ still route"
