#!/usr/bin/env bash

# Self-test for smoke.sh, in the same shape as overseer_test.sh: stubbed `curl`
# and `docker` on PATH let a case drive the go-api /health status, the /overseer/
# status, the overseer /health status+body, and the overseer log contents, so we
# can assert the gate passes only when the tier is healthy and fails (red) on each
# failure signature — the red-proof #1492 requires.

set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
FAILURES=0

# STUB_HEALTH is the code curl reports for go-api /health (default 200), STUB_OVERSEER
# for /overseer/ (default 200), STUB_OVH_CODE / STUB_OVH_BODY the status and JSON body
# for the overseer /health liveness probe (default 200 with buckets_ok=6), STUB_LOGS
# what `docker logs` emits (default clean).
setup_case() {
    local stub_health=${STUB_HEALTH:-200} stub_overseer=${STUB_OVERSEER:-200}
    local stub_ovh_code=${STUB_OVH_CODE:-200}
    local stub_ovh_body=${STUB_OVH_BODY:-'{"status":"ok","buckets_ok":6,"buckets_failed":0}'}
    local stub_logs=${STUB_LOGS:-}
    WORK=$(mktemp -d)
    mkdir -p "$WORK/bin" "$WORK/api/deploy"
    cp "$HERE/lib.sh" "$HERE/smoke.sh" "$WORK/api/deploy/"

    cat >"$WORK/bin/curl" <<EOF
#!/usr/bin/env bash
url=\${*: -1}
case "\$url" in
    */overseer/health) printf '%s\n%s' '$stub_ovh_body' '$stub_ovh_code' ;;
    */health)          printf '%s' '$stub_health' ;;
    */overseer/)       printf '%s' '$stub_overseer' ;;
esac
exit 0
EOF
    cat >"$WORK/bin/docker" <<EOF
#!/usr/bin/env bash
echo "docker \$*" >> "$WORK/actions.log"
case "\$1" in logs) printf '%s' '$stub_logs' ;; esac
exit 0
EOF
    chmod +x "$WORK/bin"/*
    : >"$WORK/actions.log"

    (cd "$WORK/api" && PATH="$WORK/bin:$PATH" \
        bash deploy/smoke.sh https://tier.example altune-overseer \
        >"$WORK/out.log" 2>&1)
    RC=$?
    unset STUB_HEALTH STUB_OVERSEER STUB_OVH_CODE STUB_OVH_BODY STUB_LOGS
}

fail() {
    printf 'FAIL: %s\n      %s\n' "$CASE" "$1"
    FAILURES=$((FAILURES + 1))
}

expect_rc() {
    [ "$RC" = "$1" ] || fail "expected exit $1, got $RC ($(tail -1 "$WORK/out.log"))"
}

expect_out() {
    grep -qF "$1" "$WORK/out.log" || fail "expected output to mention '$1'"
}

CASE="a healthy tier with overseer /health 200 and buckets_ok>=1 passes the gate"
setup_case
expect_rc 0
expect_out "smoke gate passed"

CASE="a stalled overseer /health (503) fails the gate"
STUB_OVH_CODE=503 STUB_OVH_BODY='{"status":"collect_stalled","buckets_ok":0,"buckets_failed":0}' setup_case
expect_rc 1
expect_out "did not return 200"

CASE="an all-sources-down cycle (buckets_ok=0) fails the gate"
STUB_OVH_BODY='{"status":"ok","buckets_ok":0,"buckets_failed":6}' setup_case
expect_rc 1
expect_out "buckets_ok=0"

CASE="a non-200 go-api /health fails the gate"
STUB_HEALTH=503 setup_case
expect_rc 1
expect_out "/health returned 503"

CASE="an unreachable /overseer fails the gate"
STUB_OVERSEER=502 setup_case
expect_rc 1
expect_out "/overseer/ returned 502"

CASE="a permission-denied persist failure in overseer logs fails the gate"
STUB_LOGS=$'goapi: persisting rotated refresh token failed error=open /var/lib/overseer/readonly_refresh_token: permission denied' \
    setup_case
expect_rc 1
expect_out "operator-token persistence/seed failure"

CASE="a replayed-seed refresh failure in overseer logs fails the gate"
STUB_LOGS='sb error: refresh_token_already_used' setup_case
expect_rc 1
expect_out "operator-token persistence/seed failure"

CASE="a partial-failure cycle (buckets_ok>=1, some failed) does not fail the gate"
STUB_OVH_BODY='{"status":"ok","buckets_ok":5,"buckets_failed":1}' \
    STUB_LOGS='overseer.collect.source_down bucket=oci-usage error=usage endpoint 404' \
    setup_case
expect_rc 0
expect_out "smoke gate passed"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all smoke gate checks passed\n'
