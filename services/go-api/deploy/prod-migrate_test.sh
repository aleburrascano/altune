#!/usr/bin/env bash

# Self-test for prod-migrate.sh, in the same shape as staging_test.sh: a stubbed
# `psql` on PATH records the actions the script would run and replays a tiny
# stateful schema_migrations table. Prod deliberately does NOT blind-adopt (unlike
# staging), so we assert: the env gate fails loudly BEFORE any migration; an
# already-migrated prod DB with an EMPTY tracker FAILS CLOSED (never auto-adopts,
# so a migration prod never got can't be silently skipped); an established baseline
# applies only genuinely-new migrations; a truly fresh DB applies every migration;
# and a failing migration aborts non-zero (so the caller never swaps onto an
# unmigrated schema).

set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
FAILURES=0

# env_body is the literal .env.production contents ("" + has_file=no == no file).
# STUB_TRACKS is what to_regclass('public.tracks') reports ('t' == schema present,
# 'f' == fresh empty DB). STUB_PREAPPLIED seeds schema_migrations (newline list of
# versions) so a case can model an established baseline. STUB_MIGRATE_FAIL=yes makes
# an actual `psql -f <file>` apply exit non-zero.
setup_case() {
    local env_body=$1 has_file=${2:-yes}
    local stub_tracks=${STUB_TRACKS:-t} stub_fail=${STUB_MIGRATE_FAIL:-no}
    local stub_preapplied=${STUB_PREAPPLIED:-}
    WORK=$(mktemp -d)
    mkdir -p "$WORK/bin" "$WORK/api/deploy" "$WORK/api/migrations"
    cp "$HERE/lib.sh" "$HERE/prod-migrate.sh" "$WORK/api/deploy/"
    : >"$WORK/api/migrations/001_baseline.sql"
    : >"$WORK/api/migrations/002_indexes.sql"
    : >"$WORK/api/migrations/016_constraint.sql"

    if [ "$has_file" = yes ]; then
        printf '%s\n' "$env_body" >"$WORK/api/.env.production"
    fi

    cat >"$WORK/bin/psql" <<EOF
#!/usr/bin/env bash
applied="$WORK/applied"; touch "\$applied"
query=""; prev=""; has_f=no
for a in "\$@"; do
    [ "\$prev" = "-c" ] && query="\$a"
    [ "\$a" = "-f" ] && has_f=yes
    prev="\$a"
done
# A failing migration errors on the file apply, before its tracker INSERT lands.
[ "\$has_f" = yes ] && [ "$stub_fail" = yes ] && exit 1
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
    chmod +x "$WORK/bin"/*
    [ -n "$stub_preapplied" ] && printf '%s\n' "$stub_preapplied" >"$WORK/applied"

    (cd "$WORK/api" && PATH="$WORK/bin:$PATH" \
        bash deploy/prod-migrate.sh >"$WORK/out.log" 2>&1)
    RC=$?
    unset STUB_TRACKS STUB_MIGRATE_FAIL STUB_PREAPPLIED
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

expect_no_apply() {
    grep -qE "applying prod migration [0-9]" "$WORK/out.log" && fail "$1"
}

FULL_ENV='DATABASE_URL=postgres://u:p@h:5432/db'

CASE="a missing .env.production fails before touching migrations"
setup_case "" no
expect_rc 1
expect_out ".env.production not found"
expect_no_apply "touched migrations without an env file"

CASE="an empty DATABASE_URL fails loudly before any migration"
setup_case 'DATABASE_URL='
expect_rc 1
expect_out "DATABASE_URL is unset or empty"

CASE="an already-migrated prod DB with an empty tracker fails closed, never adopts"
STUB_TRACKS=t setup_case "$FULL_ENV"
expect_rc 1
expect_out "baseline is not established"
expect_no_apply "ran a migration against an un-baselined prod DB (016 would be skipped or break)"
grep -qF "adopting existing" "$WORK/out.log" && fail "blindly adopted prod as baseline (would skip forgotten migrations)"

CASE="an established baseline applies only the genuinely-new migration"
STUB_PREAPPLIED=$'001_baseline\n002_indexes' setup_case "$FULL_ENV"
expect_rc 0
expect_out "applying prod migration 016_constraint"
grep -qE "applying prod migration 001" "$WORK/out.log" && fail "re-applied an already-tracked migration"
expect_out "prod migrations up to date"

CASE="a truly fresh empty DB applies every migration in order"
STUB_TRACKS=f setup_case "$FULL_ENV"
expect_rc 0
expect_out "applying prod migration 001_baseline"
expect_out "applying prod migration 016_constraint"
expect_out "prod migrations up to date"

CASE="a failing migration aborts non-zero before the deploy swaps"
STUB_TRACKS=f STUB_MIGRATE_FAIL=yes setup_case "$FULL_ENV"
expect_rc 1
grep -qF "prod migrations up to date" "$WORK/out.log" && fail "reported success despite a failed migration"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all prod migrate checks passed\n'
