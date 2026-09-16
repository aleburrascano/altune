#!/usr/bin/env bash

# Self-test for staging.sh, in the same shape as overseer_test.sh: stubbed `psql`,
# `docker`, and `curl` on PATH record the actions the script would run and replay a
# tiny stateful schema_migrations table, so we can assert the env gate fails loudly
# BEFORE any build, an already-migrated DB is adopted (no non-idempotent re-run), a
# fresh DB applies every migration, and an unhealthy go-api fails the deploy.

set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
FAILURES=0

# env_body is the literal .env.staging contents ("" + has_file=no == no file).
# STUB_TRACKS is what to_regclass('public.tracks') reports ('t' == already-migrated
# DB, 'f' == fresh). STUB_HEALTHY drives the public /health poll (yes == 200).
setup_case() {
    local env_body=$1 has_file=${2:-yes}
    local stub_tracks=${STUB_TRACKS:-t} stub_healthy=${STUB_HEALTHY:-yes}
    WORK=$(mktemp -d)
    mkdir -p "$WORK/bin" "$WORK/api/deploy" "$WORK/api/migrations"
    cp "$HERE/lib.sh" "$HERE/staging.sh" "$HERE/compose.staging.yml" "$WORK/api/deploy/"
    : >"$WORK/api/migrations/001_baseline.sql"
    : >"$WORK/api/migrations/002_indexes.sql"
    : >"$WORK/api/migrations/016_constraint.sql"

    if [ "$has_file" = yes ]; then
        printf '%s\n' "$env_body" >"$WORK/api/.env.staging"
    fi

    cat >"$WORK/bin/psql" <<EOF
#!/usr/bin/env bash
applied="$WORK/applied"; touch "\$applied"
query=""; prev=""
for a in "\$@"; do [ "\$prev" = "-c" ] && query="\$a"; prev="\$a"; done
case "\$query" in
    *"count(*) FROM schema_migrations"*) wc -l < "\$applied" | tr -d ' ' ;;
    *"to_regclass"*) printf '%s' '$stub_tracks' ;;
    *"SELECT 1 FROM schema_migrations WHERE version"*)
        v=\$(printf '%s' "\$query" | sed -E "s/.*version='([^']*)'.*/\1/")
        grep -qxF "\$v" "\$applied" && printf '1' ;;
    *"INSERT INTO schema_migrations"*)
        v=\$(printf '%s' "\$query" | sed -E "s/.*VALUES \('([^']*)'\).*/\1/")
        grep -qxF "\$v" "\$applied" || printf '%s\n' "\$v" >> "\$applied" ;;
esac
exit 0
EOF
    cat >"$WORK/bin/docker" <<EOF
#!/usr/bin/env bash
echo "docker \$*" >> "$WORK/actions.log"
exit 0
EOF
    printf '#!/usr/bin/env bash\nexit %s\n' "$([ "$stub_healthy" = yes ] && echo 0 || echo 1)" \
        >"$WORK/bin/curl"
    printf '#!/usr/bin/env bash\nexit 0\n' >"$WORK/bin/sleep"
    chmod +x "$WORK/bin"/*
    : >"$WORK/actions.log"

    (cd "$WORK/api" && PATH="$WORK/bin:$PATH" STAGING_HEALTH_TIMEOUT=1 \
        bash deploy/staging.sh >"$WORK/out.log" 2>&1)
    RC=$?
    unset STUB_TRACKS STUB_HEALTHY
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

expect_action() {
    grep -qF "$1" "$WORK/actions.log" || fail "expected action '$1'"
}

expect_no_action() {
    grep -qF "$1" "$WORK/actions.log" && fail "unexpected action '$1'"
}

FULL_ENV=$'DATABASE_URL=postgres://u:p@h:5432/db\nOVERSEER_SUPABASE_URL=https://x.supabase.co\nOVERSEER_SUPABASE_ANON_KEY=sb_publishable_abc\nOVERSEER_OWNER_USER_ID=955fca87-3a19-415f-b9b8-c9b934b39524'

CASE="a missing env file fails before touching migrations or containers"
setup_case "" no
expect_rc 1
expect_out ".env.staging not found"
expect_no_action "compose -f deploy/compose.staging.yml up"

CASE="a missing required var fails loudly before any build"
setup_case $'DATABASE_URL=postgres://u:p@h:5432/db\nOVERSEER_SUPABASE_URL=https://x.supabase.co\nOVERSEER_SUPABASE_ANON_KEY=y'
expect_rc 1
expect_out "OVERSEER_OWNER_USER_ID"
expect_no_action "up"

CASE="an empty-valued required var counts as missing"
setup_case $'DATABASE_URL=\nOVERSEER_SUPABASE_URL=https://x.supabase.co\nOVERSEER_SUPABASE_ANON_KEY=y\nOVERSEER_OWNER_USER_ID=z'
expect_rc 1
expect_out "DATABASE_URL"

CASE="an already-migrated DB is adopted and no migration is re-run"
STUB_TRACKS=t setup_case "$FULL_ENV"
expect_rc 0
expect_out "adopting existing staging schema"
grep -qF "applying migration" "$WORK/out.log" && fail "re-ran a migration on an already-migrated DB"
expect_action "compose -f deploy/compose.staging.yml up -d --build go-api-blue overseer redis"
expect_out "deployed staging"

CASE="a fresh DB applies every migration in order"
STUB_TRACKS=f setup_case "$FULL_ENV"
expect_rc 0
expect_out "applying migration 001_baseline"
expect_out "applying migration 016_constraint"
expect_out "deployed staging"

CASE="an unhealthy go-api fails the staging deploy"
STUB_HEALTHY=no setup_case "$FULL_ENV"
expect_rc 1
expect_out "not healthy"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all staging deploy checks passed\n'
