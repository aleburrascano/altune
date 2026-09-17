#!/usr/bin/env bash

# Self-test for prod-migrate.sh, in the same shape as staging_test.sh: a stubbed
# `psql` on PATH records the actions the script would run and replays a tiny
# stateful schema_migrations table. Prod deliberately does NOT blind-adopt (unlike
# staging), so we assert: the env gate fails loudly BEFORE any migration; an
# already-migrated prod DB with an EMPTY tracker FAILS CLOSED (never auto-adopts,
# so a migration prod never got can't be silently skipped); an established baseline
# applies only genuinely-new migrations; a truly fresh DB applies every migration;
# a no-transaction migration drops --single-transaction while a normal one keeps it
# (#1550); and a failing migration aborts non-zero without recording itself (so the
# caller never swaps onto an unmigrated schema).

set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
FAILURES=0

# env_body is the literal .env.production contents ("" + has_file=no == no file).
# STUB_TRACKS is what to_regclass('public.tracks') reports ('t' == schema present,
# 'f' == fresh empty DB). STUB_PREAPPLIED seeds schema_migrations (newline list of
# versions) so a case can model an established baseline. STUB_MIGRATE_FAIL is a
# version glob whose `psql -f <file>` apply exits non-zero ('*' == every migration).
setup_case() {
    local env_body=$1 has_file=${2:-yes}
    local stub_tracks=${STUB_TRACKS:-t} stub_fail=${STUB_MIGRATE_FAIL:-__none__}
    local stub_preapplied=${STUB_PREAPPLIED:-}
    WORK=$(mktemp -d)
    mkdir -p "$WORK/bin" "$WORK/api/deploy" "$WORK/api/migrations"
    cp "$HERE/lib.sh" "$HERE/prod-migrate.sh" "$WORK/api/deploy/"
    : >"$WORK/api/migrations/001_baseline.sql"
    : >"$WORK/api/migrations/002_indexes.sql"
    : >"$WORK/api/migrations/016_constraint.sql"
    # The three transaction routes lib.sh must tell apart (#1550): the explicit
    # marker, an unmarked CONCURRENTLY the author forgot to mark, and a file that
    # only mentions CONCURRENTLY in prose (which keeps its transaction).
    printf '%s\n' '-- migrate:no-transaction' 'SELECT 1;' \
        >"$WORK/api/migrations/020_marked_index.sql"
    printf '%s\n' 'CREATE INDEX CONCURRENTLY idx_x ON tracks (id);' \
        >"$WORK/api/migrations/021_unmarked_index.sql"
    printf '%s\n' '-- not built with CREATE INDEX CONCURRENTLY' 'SELECT 1;' \
        >"$WORK/api/migrations/022_prose_only.sql"

    if [ "$has_file" = yes ]; then
        printf '%s\n' "$env_body" >"$WORK/api/.env.production"
    fi

    cat >"$WORK/bin/psql" <<EOF
#!/usr/bin/env bash
applied="$WORK/applied"; touch "\$applied"
query=""; prev=""; file=""; single=no
for a in "\$@"; do
    [ "\$prev" = "-c" ] && query="\$a"
    [ "\$prev" = "-f" ] && file="\$a"
    [ "\$a" = "--single-transaction" ] && single=yes
    prev="\$a"
done
if [ -n "\$file" ]; then
    version=\$(basename "\$file" .sql)
    printf '%s single-transaction=%s\n' "\$version" "\$single" >> "$WORK/applies.log"
    # A failing migration errors on the file apply, and ON_ERROR_STOP keeps psql
    # from reaching the tracker INSERT that follows it in the same invocation.
    case "\$version" in $stub_fail) exit 1 ;; esac
fi
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
    : >"$WORK/applies.log"
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

expect_apply() {
    grep -qxF "$1 single-transaction=$2" "$WORK/applies.log" ||
        fail "expected $1 applied with single-transaction=$2"
}

expect_untracked() {
    grep -qxF "$1" "$WORK/applied" && fail "recorded $1 as applied despite its failed apply"
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

CASE="a no-transaction migration is applied in autocommit, a normal one is not"
STUB_TRACKS=f setup_case "$FULL_ENV"
expect_rc 0
expect_apply 001_baseline yes
expect_apply 020_marked_index no
expect_apply 021_unmarked_index no
expect_apply 022_prose_only yes
expect_out "020_marked_index is no-transaction"

CASE="a failed no-transaction migration is never recorded as applied"
STUB_TRACKS=f STUB_MIGRATE_FAIL='020_*' setup_case "$FULL_ENV"
expect_rc 1
expect_untracked 020_marked_index

CASE="a failing migration aborts non-zero before the deploy swaps"
STUB_TRACKS=f STUB_MIGRATE_FAIL='*' setup_case "$FULL_ENV"
expect_rc 1
grep -qF "prod migrations up to date" "$WORK/out.log" && fail "reported success despite a failed migration"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all prod migrate checks passed\n'
