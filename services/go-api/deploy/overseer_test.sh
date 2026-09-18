#!/usr/bin/env bash

# Self-test for overseer.sh, in the same shape as blue-green_test.sh: a stubbed
# `docker` on PATH records the actions the script would run, so we can assert the
# env-var presence gate fails loudly BEFORE any build/up, and lets a complete env
# through.

set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
FAILURES=0

# env_body is the literal contents of the fake .env.production ("" == no file). The
# stub docker replays canned responses for the post-`up` checks so a case can drive
# ownership repair and the smoke check: STUB_OWNER is what `stat` reports for the
# data dir (default 1000 == already fixed), STUB_HEALTH what `inspect` reports
# (default healthy), STUB_LOGS what `logs` emits (default clean).
setup_case() {
    local env_body=$1 has_file=${2:-yes}
    local stub_owner=${STUB_OWNER:-1000} stub_health=${STUB_HEALTH:-healthy}
    local stub_logs=${STUB_LOGS:-}
    WORK=$(mktemp -d)
    mkdir -p "$WORK/bin" "$WORK/api/deploy"
    cp "$HERE/lib.sh" "$HERE/overseer.sh" "$HERE/compose.prod.yml" "$WORK/api/deploy/"

    if [ "$has_file" = yes ]; then
        printf '%s\n' "$env_body" >"$WORK/api/.env.production"
    fi

    cat >"$WORK/bin/docker" <<EOF
#!/usr/bin/env bash
echo "docker \$*" >> "$WORK/actions.log"
case "\$1" in
    inspect) printf '%s\n' '$stub_health' ;;
    logs)    printf '%s' '$stub_logs' ;;
    exec)    case "\$*" in *stat*) printf '%s\n' '$stub_owner' ;; esac ;;
esac
exit 0
EOF
    chmod +x "$WORK/bin"/*
    : >"$WORK/actions.log"

    (cd "$WORK/api" && PATH="$WORK/bin:$PATH" OVERSEER_SMOKE_WINDOW=0 \
        bash deploy/overseer.sh >"$WORK/out.log" 2>&1)
    RC=$?
    unset STUB_OWNER STUB_HEALTH STUB_LOGS
}

fail() {
    printf 'FAIL: %s\n      %s\n' "$CASE" "$1"
    FAILURES=$((FAILURES + 1))
}

expect_rc() {
    [ "$RC" = "$1" ] || fail "expected exit $1, got $RC ($(tail -1 "$WORK/out.log"))"
}

expect_action() {
    grep -qF "$1" "$WORK/actions.log" || fail "expected action '$1'"
}

expect_no_action() {
    grep -qF "$1" "$WORK/actions.log" && fail "unexpected action '$1'"
}

expect_out() {
    grep -qF "$1" "$WORK/out.log" || fail "expected output to mention '$1'"
}

FULL_ENV=$'OVERSEER_OWNER_USER_ID=955fca87-3a19-415f-b9b8-c9b934b39524\nOVERSEER_SUPABASE_URL=https://x.supabase.co\nOVERSEER_SUPABASE_ANON_KEY=sb_publishable_abc'

CASE="a complete env builds and recreates overseer"
setup_case "$FULL_ENV"
expect_rc 0
expect_action "build overseer"
expect_action "up -d overseer"

CASE="an already-owned data dir is not chowned and the deploy passes its smoke check"
setup_case "$FULL_ENV"
expect_rc 0
expect_no_action "chown overseer:overseer"
expect_no_action "up -d --force-recreate overseer"
expect_out "smoke check passed"

CASE="a root-owned data dir is chowned and overseer is recreated"
STUB_OWNER=0 setup_case "$FULL_ENV"
expect_rc 0
expect_action "chown overseer:overseer /var/lib/overseer"
expect_action "up -d --force-recreate overseer"

CASE="a permission-denied persist failure in the logs fails the deploy"
STUB_LOGS=$'goapi: persisting rotated refresh token failed error=open /var/lib/overseer/readonly_refresh_token: permission denied' \
    setup_case "$FULL_ENV"
expect_rc 1
expect_out "operator-token persistence/seed failure"

CASE="a replayed-seed refresh failure in the logs fails the deploy"
STUB_LOGS='sb error: refresh_token_already_used' setup_case "$FULL_ENV"
expect_rc 1
expect_out "operator-token persistence/seed failure"

CASE="an unrelated collect.failed does not fail the deploy"
STUB_LOGS='overseer.collect.failed bucket=oci-usage error=usage endpoint 404' \
    setup_case "$FULL_ENV"
expect_rc 0
expect_out "smoke check passed"

CASE="an unhealthy overseer fails the deploy"
STUB_HEALTH=unhealthy setup_case "$FULL_ENV"
expect_rc 1
expect_out "not healthy"

CASE="a missing required var fails before touching the container"
setup_case $'OVERSEER_OWNER_USER_ID=x\nOVERSEER_SUPABASE_URL=https://x.supabase.co'
expect_rc 1
expect_out "OVERSEER_SUPABASE_ANON_KEY"
expect_no_action "build overseer"
expect_no_action "up -d overseer"

CASE="an empty-valued var counts as missing"
setup_case $'OVERSEER_OWNER_USER_ID=x\nOVERSEER_SUPABASE_URL=\nOVERSEER_SUPABASE_ANON_KEY=y'
expect_rc 1
expect_out "OVERSEER_SUPABASE_URL"
expect_no_action "build overseer"

CASE="a whitespace-only value counts as missing"
setup_case "$(printf 'OVERSEER_OWNER_USER_ID=x\nOVERSEER_SUPABASE_URL=   \nOVERSEER_SUPABASE_ANON_KEY=y')"
expect_rc 1
expect_out "OVERSEER_SUPABASE_URL"

CASE="a missing env file fails loudly"
setup_case "" no
expect_rc 1
expect_out ".env.production not found"
expect_no_action "build overseer"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all overseer deploy checks passed\n'
