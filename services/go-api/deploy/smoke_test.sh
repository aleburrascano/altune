#!/usr/bin/env bash


set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
parent=$(mktemp -d)
trap 'rm -rf "$parent"' EXIT
FAILURES=0

setup_case() {
    local stub_health=${STUB_HEALTH:-200} stub_overseer=${STUB_OVERSEER:-200}
    local stub_ovh_code=${STUB_OVH_CODE:-200}
    local stub_ovh_body=${STUB_OVH_BODY:-'{"status":"ok","buckets_ok":6,"buckets_failed":0}'}
    local stub_logs=${STUB_LOGS:-}
    local stub_journey_rc=${STUB_JOURNEY_RC:-0}
    local stub_journey_out=${STUB_JOURNEY_OUT:-'journey-check: search ok (10 results)'}
    local stub_version=${STUB_VERSION:-deadbeef}
    local stub_health_body=${STUB_HEALTH_BODY:-'{"status":"ok","version":"'"$stub_version"'"}'}
    local expected_commit=${SMOKE_EXPECTED_COMMIT:-}
    WORK=$(mktemp -d -p "$parent")
    mkdir -p "$WORK/bin" "$WORK/api/deploy"
    cp "$HERE/lib.sh" "$HERE/smoke.sh" "$WORK/api/deploy/"

    cat >"$WORK/bin/curl" <<EOF
#!/usr/bin/env bash
url=\${*: -1}
case "\$url" in
    */overseer/health) printf '%s\n%s' '$stub_ovh_body' '$stub_ovh_code' ;;
    */health)
        case "\$*" in
            *"-o /dev/null"*) printf '%s' '$stub_health' ;;
            *)                printf '%s' '$stub_health_body' ;;
        esac
        ;;
    */overseer/)       printf '%s' '$stub_overseer' ;;
esac
exit 0
EOF
    cat >"$WORK/bin/docker" <<EOF
#!/usr/bin/env bash
echo "docker \$*" >> "$WORK/actions.log"
case "\$1" in logs) printf '%s' '$stub_logs' ;; esac
case "\$1" in exec) printf '%s\n' '$stub_journey_out'; exit $stub_journey_rc ;; esac
exit 0
EOF
    chmod +x "$WORK/bin"/*
    : >"$WORK/actions.log"

    (cd "$WORK/api" && PATH="$WORK/bin:$PATH" \
        bash deploy/smoke.sh https://tier.example altune-overseer ${expected_commit:+"$expected_commit"} \
        >"$WORK/out.log" 2>&1)
    RC=$?
    unset STUB_HEALTH STUB_OVERSEER STUB_OVH_CODE STUB_OVH_BODY STUB_LOGS
    unset STUB_JOURNEY_RC STUB_JOURNEY_OUT SMOKE_GOAPI_CONTAINER
    unset STUB_VERSION STUB_HEALTH_BODY SMOKE_EXPECTED_COMMIT
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

CASE="a passing journey-check runs in the default staging go-api container and passes the gate"
setup_case
expect_rc 0
expect_out "smoke gate passed"
grep -qF "docker exec altune-staging-go-api-blue /app journey-check" "$WORK/actions.log" ||
    fail "expected journey-check to run in altune-staging-go-api-blue"

CASE="SMOKE_GOAPI_CONTAINER picks the go-api container journey-check runs in"
SMOKE_GOAPI_CONTAINER=altune-go-api-green setup_case
expect_rc 0
grep -qF "docker exec altune-go-api-green /app journey-check" "$WORK/actions.log" ||
    fail "expected journey-check to run in altune-go-api-green"

CASE="a failed journey-check download fails the gate and surfaces why"
STUB_JOURNEY_RC=1 \
    STUB_JOURNEY_OUT='ERROR: journey-check: download failed: HTTP Error 403: Forbidden' \
    setup_case
expect_rc 1
expect_out "FAILED: journey-check"
expect_out "download failed: HTTP Error 403"

CASE="a failed journey-check search fails the gate"
STUB_JOURNEY_RC=1 STUB_JOURNEY_OUT='ERROR: journey-check: search failed: no results' setup_case
expect_rc 1
expect_out "FAILED: journey-check"
expect_out "search failed: no results"

CASE="a journey-check exec that cannot reach the container (docker exit 125) fails the gate"
STUB_JOURNEY_RC=125 STUB_JOURNEY_OUT='Error response from daemon: container is not running' setup_case
expect_rc 1
expect_out "FAILED: journey-check"

CASE="a journey-check exec that outruns its timeout wrapper (exit 124) fails the gate"
STUB_JOURNEY_RC=124 STUB_JOURNEY_OUT='' setup_case
expect_rc 1
expect_out "FAILED: journey-check"
expect_out "timed out after"

CASE="no expected-commit argument leaves version-checking behaviour unchanged"
setup_case
expect_rc 0
expect_out "smoke gate passed"

CASE="a matching expected-commit passes the gate"
STUB_VERSION=cafef00d SMOKE_EXPECTED_COMMIT=cafef00d setup_case
expect_rc 0
expect_out "smoke gate passed"

CASE="a mismatched expected-commit fails the gate"
STUB_VERSION=cafef00d SMOKE_EXPECTED_COMMIT=baadf00d setup_case
expect_rc 1
expect_out "FAILED: running version cafef00d, expected baadf00d"

CASE="a missing version field fails the gate when a commit is expected"
STUB_HEALTH_BODY='{"status":"ok"}' SMOKE_EXPECTED_COMMIT=baadf00d setup_case
expect_rc 1
expect_out "FAILED: running version <missing>, expected baadf00d"

CASE="every operator-token failure signature in overseer logs fails the gate"
for sig in 'goapi: persisting rotated refresh token failed error=disk full' \
    'goapi: read-only token refresh failed at status: status 400' \
    'goapi: read-only token refresh failed at password_grant: invalid credentials' \
    'open /var/lib/overseer/readonly_refresh_token: permission denied' \
    'sb error: refresh_token_already_used'; do
    STUB_LOGS=$sig setup_case
    expect_rc 1
    expect_out "operator-token persistence/seed failure"
done

CASE="a read-only token refresh at a status other than 400 does not fail the gate"
STUB_LOGS='goapi: read-only token refresh failed at status: status 401' setup_case
expect_rc 0
expect_out "smoke gate passed"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all smoke gate checks passed\n'
