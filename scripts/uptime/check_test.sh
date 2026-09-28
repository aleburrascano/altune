#!/usr/bin/env bash


set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
FAILURES=0
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

mkdir -p "$WORK/bin"
cat >"$WORK/bin/curl" <<'STUB'
#!/usr/bin/env bash
out=/dev/null
url=${*: -1}
while [ $# -gt 0 ]; do
    case "$1" in -o) out=$2; shift ;; esac
    shift
done
case "$url" in
    */health) code=$STUB_HEALTH_CODE body= ;;
    */auth/v1/token*) code=200 body='{"access_token":"tok"}' ;;
    */v1/discovery/search*) code=$STUB_SEARCH_CODE body='{"results":[{"title":"x"}]}' ;;
    *) code=404 body= ;;
esac
[ "$out" = /dev/null ] || printf '%s' "$body" >"$out"
printf '%s' "$code"
STUB
chmod +x "$WORK/bin/curl"

cat >"$WORK/uptime.env" <<'ENV'
UPTIME_HEALTH_URL=https://api.example/health
ENV

fail() {
    printf 'FAIL: %s\n      %s\n' "$CASE" "$1"
    FAILURES=$((FAILURES + 1))
}

run_check() {
    env PATH="$WORK/bin:$PATH" STUB_DIR="$WORK" HOME="$WORK/home" \
        UPTIME_ENV_FILE="$WORK/uptime.env" \
        STUB_HEALTH_CODE="${STUB_HEALTH_CODE:-200}" STUB_SEARCH_CODE="${STUB_SEARCH_CODE:-200}" \
        bash "$HERE/check.sh" >"$WORK/out.log" 2>&1
    RC=$?
    unset STUB_HEALTH_CODE STUB_SEARCH_CODE
}

expect_rc() {
    [ "$RC" = "$1" ] || fail "expected exit $1, got $RC ($(cat "$WORK/out.log"))"
}

CASE="a healthy box exits 0"
run_check
expect_rc 0
grep -qF ' up: ' "$WORK/out.log" || fail "log line does not say up: $(cat "$WORK/out.log")"

CASE="a failed /health exits 1 and logs the failure"
STUB_HEALTH_CODE=502 run_check
expect_rc 1
grep -qF ' down: readiness probe → HTTP 502' "$WORK/out.log" ||
    fail "log line does not say down with the status: $(cat "$WORK/out.log")"

CASE="the journey is skipped while UPTIME_PROBE_EMAIL is unset"
STUB_SEARCH_CODE=503 run_check
expect_rc 0

cat >>"$WORK/uptime.env" <<'ENV'
UPTIME_SUPABASE_URL=https://proj.supabase.example
UPTIME_SUPABASE_ANON_KEY=anon
UPTIME_PROBE_EMAIL=uptime-probe@altune.invalid
UPTIME_PROBE_PASSWORD=pw
ENV

CASE="a configured journey that fails marks the box down"
STUB_SEARCH_CODE=503 run_check
expect_rc 1
grep -qF 'search failed (HTTP 503)' "$WORK/out.log" || fail "log does not name the journey stage: $(cat "$WORK/out.log")"

CASE="a configured journey that passes is up"
run_check
expect_rc 0
grep -qF 'search 200 with 1 results' "$WORK/out.log" || fail "journey did not run: $(cat "$WORK/out.log")"

CASE="an unset UPTIME_HEALTH_URL is down, not up"
: >"$WORK/uptime.env"
run_check
expect_rc 1

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%d uptime check(s) failed\n' "$FAILURES"
    exit 1
fi
printf 'uptime check: all checks passed\n'
