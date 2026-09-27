#!/usr/bin/env bash

set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
FAILURES=0

fail() {
    printf 'FAIL: %s\n      %s\n' "$CASE" "$1"
    FAILURES=$((FAILURES + 1))
}

expect_rc() {
    [ "$RC" = "$1" ] || fail "expected exit $1, got $RC ($OUT)"
}

expect_out() {
    grep -qF "$1" <<<"$OUT" || fail "expected output to mention '$1' (got: $OUT)"
}

expect_no_out() {
    grep -qF "$1" <<<"$OUT" && fail "did not expect output to mention '$1' (got: $OUT)"
    return 0
}

run_case() {
    local dir=$1
    OUT=$(bash "$HERE/migration-safety.sh" "$dir" 2>&1)
    RC=$?
}

write_fixture() {
    local dir=$1 name=$2 body=$3
    printf '%s\n' "$body" >"$dir/$name"
}

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

CASE="DROP TABLE without a contract marker fails"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 025_x.sql 'DROP TABLE IF EXISTS widgets;'
run_case "$DIR"
expect_rc 1
expect_out "DROP TABLE needs expand-contract"

CASE="DROP COLUMN without a contract marker fails"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 025_x.sql 'ALTER TABLE widgets DROP COLUMN legacy;'
run_case "$DIR"
expect_rc 1
expect_out "DROP COLUMN needs expand-contract"

CASE="RENAME without a contract marker fails"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 025_x.sql 'ALTER TABLE widgets RENAME TO gadgets;'
run_case "$DIR"
expect_rc 1
expect_out "RENAME needs expand-contract"

CASE="SET NOT NULL without a contract marker fails"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 025_x.sql 'ALTER TABLE widgets ALTER COLUMN name SET NOT NULL;'
run_case "$DIR"
expect_rc 1
expect_out "SET NOT NULL needs expand-contract"

CASE="ADD COLUMN NOT NULL without DEFAULT fails"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 025_x.sql 'ALTER TABLE widgets ADD COLUMN weight INT NOT NULL;'
run_case "$DIR"
expect_rc 1
expect_out "ADD COLUMN ... NOT NULL without DEFAULT needs expand-contract"

CASE="ADD COLUMN NOT NULL with a DEFAULT in the same clause passes"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 025_x.sql 'ALTER TABLE widgets ADD COLUMN weight INT NOT NULL DEFAULT 0;'
run_case "$DIR"
expect_rc 0

CASE="a -- contract marker exempts an otherwise unsafe file"
DIR=$(mktemp -d -p "$WORK")
printf '%s\n%s\n' 'DROP TABLE IF EXISTS widgets;' '-- contract' >"$DIR/025_x.sql"
run_case "$DIR"
expect_rc 0

CASE="versions below 025 are ignored even with unsafe patterns"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 024_x.sql 'DROP TABLE IF EXISTS widgets;'
run_case "$DIR"
expect_rc 0

CASE="a pattern inside a -- comment line is ignored"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 025_x.sql '-- DROP TABLE widgets is not actually run here'
run_case "$DIR"
expect_rc 0
expect_no_out "needs expand-contract"

CASE="CREATE TABLE ... NOT NULL and DROP INDEX are allowed"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 025_x.sql 'CREATE TABLE widgets (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL); DROP INDEX IF EXISTS idx_old;'
run_case "$DIR"
expect_rc 0

CASE="a missing migrations directory fails loudly"
run_case "$WORK/does-not-exist"
expect_rc 1
expect_out "no such directory"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all migration safety checks passed\n'
