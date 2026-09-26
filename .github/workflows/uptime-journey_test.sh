#!/usr/bin/env bash

# Self-test for uptime-journey.sh and the uptime-check.yml probe job, in the same
# shape as deploy-backend_test.sh. A stubbed `curl` on PATH drives the Supabase
# sign-in and the search responses and records every request (argv and stdin),
# so a case asserts the probe goes red on each failure stage, green only on a
# 200 with results, and never prints the password or the access token. The
# workflow checks read uptime-check.yml itself, so what is asserted is what
# Actions runs.
#
#   bash .github/workflows/uptime-journey_test.sh

set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
FAILURES=0
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

PASSWORD='s3cret-"probe"-pw'
TOKEN='eyJ.probe-access-token.sig'
SIGNIN_BODY="{\"access_token\":\"$TOKEN\"}"
RESULTS_BODY='{"code":"","results":[{"title":"Bohemian Rhapsody"}],"total":1}'

mkdir -p "$WORK/bin"
cat >"$WORK/bin/curl" <<'EOF'
#!/usr/bin/env bash
out=
url=${*: -1}
echo "$*" >>"$STUB_DIR/argv.log"
while [ $# -gt 0 ]; do
    case "$1" in -o) out=$2; shift ;; esac
    shift
done
{ echo "ARGV $url"; cat; echo; } >>"$STUB_DIR/requests.log"
case "$url" in
    */auth/v1/token*) code=$STUB_SIGNIN_CODE body=$STUB_SIGNIN_BODY ;;
    */v1/discovery/search*) code=$STUB_SEARCH_CODE body=$STUB_SEARCH_BODY ;;
    *) code=404 body= ;;
esac
[ "$code" = "000" ] && { printf '000'; exit 7; }
printf '%s' "$body" >"$out"
printf '%s' "$code"
exit "${STUB_CURL_EXIT:-0}"
EOF
chmod +x "$WORK/bin/curl"

fail() {
    printf 'FAIL: %s\n      %s\n' "$CASE" "$1"
    FAILURES=$((FAILURES + 1))
}

run_probe() {
    : >"$WORK/requests.log"
    : >"$WORK/argv.log"
    env PATH="$WORK/bin:$PATH" STUB_DIR="$WORK" STUB_CURL_EXIT="${STUB_CURL_EXIT:-0}" \
        UPTIME_HEALTH_URL="${UPTIME_HEALTH_URL-https://api.example/health}" \
        UPTIME_SUPABASE_URL="${UPTIME_SUPABASE_URL-https://proj.supabase.example}" \
        UPTIME_SUPABASE_ANON_KEY="${UPTIME_SUPABASE_ANON_KEY-anon-key}" \
        UPTIME_PROBE_EMAIL="${UPTIME_PROBE_EMAIL-uptime-probe@altune.invalid}" \
        UPTIME_PROBE_PASSWORD="${UPTIME_PROBE_PASSWORD-$PASSWORD}" \
        STUB_SIGNIN_CODE="${STUB_SIGNIN_CODE:-200}" \
        STUB_SIGNIN_BODY="${STUB_SIGNIN_BODY-$SIGNIN_BODY}" \
        STUB_SEARCH_CODE="${STUB_SEARCH_CODE:-200}" \
        STUB_SEARCH_BODY="${STUB_SEARCH_BODY-$RESULTS_BODY}" \
        bash "$HERE/uptime-journey.sh" >"$WORK/out.log" 2>&1
    RC=$?
    unset STUB_CURL_EXIT UPTIME_HEALTH_URL UPTIME_SUPABASE_URL UPTIME_SUPABASE_ANON_KEY \
        UPTIME_PROBE_EMAIL UPTIME_PROBE_PASSWORD \
        STUB_SIGNIN_CODE STUB_SIGNIN_BODY STUB_SEARCH_CODE STUB_SEARCH_BODY
}

expect_rc() {
    [ "$RC" = "$1" ] || fail "expected exit $1, got $RC ($(tail -1 "$WORK/out.log"))"
}

expect_out() {
    grep -qF -- "$1" "$WORK/out.log" || fail "expected output to mention '$1', got: $(cat "$WORK/out.log")"
}

expect_no_secrets_printed() {
    ! grep -qF -- "$PASSWORD" "$WORK/out.log" || fail "the probe password was printed"
    ! grep -qF -- "$TOKEN" "$WORK/out.log" || fail "the access token was printed"
    ! grep -qF -e "$PASSWORD" -e "$TOKEN" "$WORK/argv.log" ||
        fail "a secret was passed to curl on argv"
}

expect_request() {
    grep -qF -- "$1" "$WORK/requests.log" || fail "expected a request carrying '$1'"
}

CASE="a 200 search with results passes, signed in as the probe account"
run_probe
expect_rc 0
expect_out "search 200 with 1 results"
expect_request "ARGV https://proj.supabase.example/auth/v1/token?grant_type=password"
expect_request '"password": "s3cret-\"probe\"-pw"'
expect_request "ARGV https://api.example/v1/discovery/search?q=Bohemian%20Rhapsody&save_history=false"
expect_request "Authorization: Bearer $TOKEN"
expect_no_secrets_printed

CASE="a rejected sign-in fails at the sign-in stage and never searches"
STUB_SIGNIN_CODE=400 STUB_SIGNIN_BODY='{"error":"invalid_grant"}' run_probe
expect_rc 1
expect_out "::error::find-music journey probe: sign-in failed (HTTP 400)"
grep -qF "/v1/discovery/search" "$WORK/requests.log" && fail "searched after a failed sign-in"
expect_no_secrets_printed

CASE="an unreachable Supabase fails at the sign-in stage"
STUB_SIGNIN_CODE=000 run_probe
expect_rc 1
expect_out "sign-in failed (HTTP 000)"

CASE="a 200 sign-in without an access_token fails at the sign-in stage"
STUB_SIGNIN_BODY='{"user":{}}' run_probe
expect_rc 1
expect_out "sign-in answered 200 without an access_token"

CASE="a non-200 search fails at the search stage, naming the API's code"
STUB_SEARCH_CODE=503 STUB_SEARCH_BODY='{"code":"all_providers_failed","results":[]}' run_probe
expect_rc 1
expect_out "::error::find-music journey probe: search failed (HTTP 503, all_providers_failed)"
expect_no_secrets_printed

CASE="a rejected token (401) fails at the search stage"
STUB_SEARCH_CODE=401 STUB_SEARCH_BODY='not json' run_probe
expect_rc 1
expect_out "search failed (HTTP 401)"

CASE="a sign-in that timed out after its 200 status line fails"
STUB_CURL_EXIT=28 run_probe
expect_rc 1
expect_out "sign-in failed (HTTP 000)"

CASE="a 200 search with zero results fails"
STUB_SEARCH_BODY='{"code":"","results":[],"total":0}' run_probe
expect_rc 1
expect_out "::error::find-music journey probe: search answered 200 with zero results"

CASE="a 200 search with an unreadable body fails"
STUB_SEARCH_BODY='<html>bad gateway</html>' run_probe
expect_rc 1
expect_out "search answered 200 with an unreadable body"

for secret in UPTIME_HEALTH_URL UPTIME_SUPABASE_URL UPTIME_SUPABASE_ANON_KEY \
    UPTIME_PROBE_EMAIL UPTIME_PROBE_PASSWORD; do
    CASE="an unset ${secret} fails naming it, before any request"
    declare "$secret="
    run_probe
    expect_rc 1
    expect_out "::error::find-music journey probe: secret ${secret} is not set"
    [ -s "$WORK/requests.log" ] && fail "made a request with ${secret} unset"
done

WORKFLOW="$HERE/uptime-check.yml"

# lift_run <step name> -> the step's `run: |` block, dedented, as Actions runs it.
lift_run() {
    awk -v name="      - name: $1" '
        $0 == name { in_step = 1; next }
        in_step && /^      - / { exit }
        in_step && /^        run: \|$/ { in_run = 1; next }
        in_run && /^          / { sub(/^          /, ""); print; next }
        in_run && NF { exit }
    ' "$WORKFLOW"
}

HEALTH_STEP="$WORK/health.sh"
lift_run "Probe readiness and fail on failure" >"$HEALTH_STEP"

CASE="the /health step fails with ::error:: when UPTIME_HEALTH_URL is unset"
if ! grep -q 'curl' "$HEALTH_STEP"; then
    fail "could not lift the /health step out of uptime-check.yml"
else
    HEALTH_URL='' PATH="$WORK/bin:$PATH" bash "$HEALTH_STEP" >"$WORK/out.log" 2>&1
    RC=$?
    expect_rc 1
    expect_out "::error::UPTIME_HEALTH_URL"
fi

CASE="the probe job runs uptime-journey.sh after the /health step with all five secrets"
health_line=$(grep -n 'name: Probe readiness and fail on failure' "$WORKFLOW" | cut -d: -f1)
journey_line=$(grep -n 'run: bash .github/workflows/uptime-journey.sh' "$WORKFLOW" | cut -d: -f1)
if [ -z "$health_line" ] || [ -z "$journey_line" ]; then
    fail "uptime-check.yml does not run both the /health step and uptime-journey.sh"
elif [ "$journey_line" -le "$health_line" ]; then
    fail "uptime-journey.sh runs before the /health step"
fi
for secret in UPTIME_HEALTH_URL UPTIME_SUPABASE_URL UPTIME_SUPABASE_ANON_KEY \
    UPTIME_PROBE_EMAIL UPTIME_PROBE_PASSWORD; do
    grep -qF "${secret}: \${{ secrets.${secret} }}" "$WORKFLOW" ||
        fail "the journey step is not passed ${secret}"
done

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%d uptime-journey check(s) failed\n' "$FAILURES"
    exit 1
fi
printf 'uptime-journey: all checks passed\n'
