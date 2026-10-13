#!/usr/bin/env bash


set -uo pipefail

HERE=$(cd "$(dirname "$0")/.." && pwd)
parent=$(mktemp -d)
trap 'rm -rf "$parent"' EXIT
FAILURES=0

setup_case() {
    local env_body=$1 has_file=${2:-yes}
    local stub_tracks=${STUB_TRACKS:-t} stub_fail=${STUB_MIGRATE_FAIL:-__none__}
    local stub_preapplied=${STUB_PREAPPLIED:-} stub_dump_rc=${STUB_DUMP_RC:-0}
    WORK=$(mktemp -d -p "$parent")
    mkdir -p "$WORK/bin" "$WORK/api/deploy" "$WORK/api/migrations"
    cp "$HERE/prod_host.sh" "$HERE/prod-migrate.sh" "$WORK/api/deploy/"
    : >"$WORK/api/migrations/001_baseline.sql"
    : >"$WORK/api/migrations/002_indexes.sql"
    : >"$WORK/api/migrations/016_constraint.sql"
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
    cat >"$WORK/bin/pg_dump" <<EOF
#!/usr/bin/env bash
printf '%s\\n' "\$*" >> "$WORK/dumps.log"
wc -l < "$WORK/applies.log" | tr -d ' ' >> "$WORK/dump-applies-seen.log"
prev=""
for a in "\$@"; do
    [ "\$prev" = "--file" ] && : >"\$a"
    prev="\$a"
done
exit $stub_dump_rc
EOF
    chmod +x "$WORK/bin"/*
    mkdir -p "$WORK/snapshots"
    for name in ${STUB_OLD_SNAPSHOTS:-}; do : >"$WORK/snapshots/$name"; done
    : >"$WORK/applies.log"
    [ -n "$stub_preapplied" ] && printf '%s\n' "$stub_preapplied" >"$WORK/applied"

    (cd "$WORK/api" && PATH="$WORK/bin:$PATH" SNAPSHOT_DIR="$WORK/snapshots" \
        bash deploy/prod-migrate.sh >"$WORK/out.log" 2>&1)
    RC=$?
    unset STUB_TRACKS STUB_MIGRATE_FAIL STUB_PREAPPLIED STUB_DUMP_RC STUB_OLD_SNAPSHOTS
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

CASE="a pending migration is snapshotted once, in custom format, before any apply"
STUB_PREAPPLIED=$'001_baseline\n002_indexes' setup_case "$FULL_ENV"
expect_rc 0
[ "$(wc -l <"$WORK/dumps.log")" = 1 ] || fail "expected exactly one pg_dump call"
grep -qF -e "--format=custom" "$WORK/dumps.log" || fail "pg_dump was not called with --format=custom"
grep -qF "before-016_constraint.dump" "$WORK/dumps.log" || fail "snapshot not named for the first pending version"
grep -qF "postgres://u:p@h:5432/db" "$WORK/dumps.log" || fail "pg_dump was not given the database url"
[ "$(cat "$WORK/dump-applies-seen.log")" = 0 ] || fail "snapshot was taken after a migration was applied"

CASE="the snapshot dir is mode 700 and the dump mode 600, even when the dir pre-existed wider"
chmod 755 "$WORK/snapshots" 2>/dev/null
STUB_PREAPPLIED=$'001_baseline\n002_indexes' setup_case "$FULL_ENV"
expect_rc 0
[ "$(stat -c %a "$WORK/snapshots")" = 700 ] || fail "snapshot dir is not mode 700"
for f in "$WORK"/snapshots/*.dump; do [ "$(stat -c %a "$f")" = 600 ] || fail "$f is not mode 600"; done

CASE="a failing pg_dump exits 1 with FAILED and applies nothing"
STUB_DUMP_RC=1 STUB_PREAPPLIED=$'001_baseline\n002_indexes' setup_case "$FULL_ENV"
expect_rc 1
expect_out "FAILED: snapshot"
expect_out "before-016_constraint.dump"
[ ! -s "$WORK/applies.log" ] || fail "applied a migration after the snapshot failed"
[ -z "$(ls "$WORK/snapshots")" ] || fail "left a partial snapshot behind"

CASE="no pending migration writes no snapshot"
STUB_PREAPPLIED=$'001_baseline\n002_indexes\n016_constraint\n020_marked_index\n021_unmarked_index\n022_prose_only' setup_case "$FULL_ENV"
expect_rc 0
[ ! -e "$WORK/dumps.log" ] || fail "pg_dump called with nothing pending"

CASE="only the newest 5 snapshots of a tier remain"
STUB_OLD_SNAPSHOTS="prod-20200101T000000Z-before-a.dump prod-20200102T000000Z-before-a.dump prod-20200103T000000Z-before-a.dump prod-20200104T000000Z-before-a.dump prod-20200105T000000Z-before-a.dump prod-20200106T000000Z-before-a.dump staging-20190101T000000Z-before-a.dump" \
    STUB_PREAPPLIED=$'001_baseline\n002_indexes' setup_case "$FULL_ENV"
expect_rc 0
prod_dumps=("$WORK"/snapshots/prod-*.dump)
[ "${#prod_dumps[@]}" = 5 ] || fail "expected 5 prod snapshots, got ${#prod_dumps[@]}"
[ ! -e "$WORK/snapshots/prod-20200101T000000Z-before-a.dump" ] || fail "oldest snapshot kept"
[ -e "$WORK/snapshots/prod-20200106T000000Z-before-a.dump" ] || fail "newer old snapshot deleted"
[ -e "$WORK/snapshots/staging-20190101T000000Z-before-a.dump" ] || fail "other tier's snapshot deleted"

CASE="restore-snapshot.sh with no arguments prints usage and exits 2"
OUT=$(bash "$HERE/restore-snapshot.sh" 2>&1); RC=$?
[ "$RC" = 2 ] || fail "expected exit 2, got $RC"
[ "$OUT" = "usage: restore-snapshot.sh <staging|prod> <dump file>" ] || fail "unexpected usage line: $OUT"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all prod migrate checks passed\n'
