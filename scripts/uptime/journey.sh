#!/usr/bin/env bash


set -uo pipefail

SEARCH_QUERY='Bohemian%20Rhapsody'

fail() {
    echo "::error::find-music journey probe: $1"
    exit 1
}

for name in UPTIME_HEALTH_URL UPTIME_SUPABASE_URL UPTIME_SUPABASE_ANON_KEY \
    UPTIME_PROBE_EMAIL UPTIME_PROBE_PASSWORD; do
    [ -n "${!name:-}" ] || fail "secret ${name} is not set in the uptime environment"
done

api_base=${UPTIME_HEALTH_URL%/}
api_base=${api_base%/health}
supabase_url=${UPTIME_SUPABASE_URL%/}

body=$(mktemp)
trap 'rm -f "$body"' EXIT

credentials=$(jq -n --arg email "$UPTIME_PROBE_EMAIL" --arg password "$UPTIME_PROBE_PASSWORD" \
    '{email: $email, password: $password}')
code=$(curl -s -o "$body" -w '%{http_code}' --max-time 20 -X POST \
    -H "apikey: ${UPTIME_SUPABASE_ANON_KEY}" -H 'Content-Type: application/json' \
    --data-binary @- "${supabase_url}/auth/v1/token?grant_type=password" \
    <<<"$credentials") || code=000
[ "$code" = "200" ] || fail "sign-in failed (HTTP ${code})"
token=$(jq -er '.access_token | select(length > 0)' "$body" 2>/dev/null) ||
    fail "sign-in answered 200 without an access_token"

code=$(curl -s -o "$body" -w '%{http_code}' --max-time 30 -H @- \
    "${api_base}/v1/discovery/search?q=${SEARCH_QUERY}&save_history=false" \
    <<<"Authorization: Bearer ${token}") || code=000
if [ "$code" != "200" ]; then
    reason=$(jq -r '.code // empty' "$body" 2>/dev/null)
    fail "search failed (HTTP ${code}${reason:+, ${reason}})"
fi
results=$(jq -er 'select((.results | type) == "array") | (.results | length)' "$body" 2>/dev/null) ||
    fail "search answered 200 with an unreadable body"
[ "$results" -gt 0 ] || fail "search answered 200 with zero results"

echo "find-music journey probe → sign-in 200, search 200 with ${results} results"
