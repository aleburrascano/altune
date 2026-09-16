#!/usr/bin/env bash

# Self-test for overseer.sh, in the same shape as blue-green_test.sh: a stubbed
# `docker` on PATH records the actions the script would run, so we can assert the
# env-var presence gate fails loudly BEFORE any build/up, and lets a complete env
# through.

set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
FAILURES=0

# env_body is the literal contents of the fake .env.production ("" == no file).
setup_case() {
    local env_body=$1 has_file=${2:-yes}
    WORK=$(mktemp -d)
    mkdir -p "$WORK/bin" "$WORK/api/deploy"
    cp "$HERE/lib.sh" "$HERE/overseer.sh" "$HERE/compose.prod.yml" "$WORK/api/deploy/"

    if [ "$has_file" = yes ]; then
        printf '%s\n' "$env_body" >"$WORK/api/.env.production"
    fi

    cat >"$WORK/bin/docker" <<EOF
#!/usr/bin/env bash
echo "docker \$*" >> "$WORK/actions.log"
exit 0
EOF
    chmod +x "$WORK/bin"/*
    : >"$WORK/actions.log"

    (cd "$WORK/api" && PATH="$WORK/bin:$PATH" \
        bash deploy/overseer.sh >"$WORK/out.log" 2>&1)
    RC=$?
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
