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
write_fixture "$DIR" 026_x.sql 'DROP TABLE IF EXISTS widgets;'
run_case "$DIR"
expect_rc 1
expect_out "DROP TABLE needs expand-contract"

CASE="DROP COLUMN without a contract marker fails"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'ALTER TABLE widgets DROP COLUMN legacy;'
run_case "$DIR"
expect_rc 1
expect_out "DROP COLUMN needs expand-contract"

CASE="RENAME without a contract marker fails"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'ALTER TABLE widgets RENAME TO gadgets;'
run_case "$DIR"
expect_rc 1
expect_out "RENAME needs expand-contract"

CASE="SET NOT NULL without a contract marker fails"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'ALTER TABLE widgets ALTER COLUMN name SET NOT NULL;'
run_case "$DIR"
expect_rc 1
expect_out "SET NOT NULL needs expand-contract"

CASE="ADD COLUMN NOT NULL without DEFAULT fails"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'ALTER TABLE widgets ADD COLUMN weight INT NOT NULL;'
run_case "$DIR"
expect_rc 1
expect_out "ADD COLUMN ... NOT NULL without DEFAULT needs expand-contract"

CASE="ADD COLUMN NOT NULL with a DEFAULT in the same clause passes"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'ALTER TABLE widgets ADD COLUMN weight INT NOT NULL DEFAULT 0;'
run_case "$DIR"
expect_rc 0

CASE="a -- contract marker exempts an otherwise unsafe file"
DIR=$(mktemp -d -p "$WORK")
printf '%s\n%s\n' 'DROP TABLE IF EXISTS widgets;' '-- contract' >"$DIR/026_x.sql"
run_case "$DIR"
expect_rc 0

CASE="versions below 026 are ignored even with unsafe patterns"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 025_x.sql 'DROP TABLE IF EXISTS widgets;'
run_case "$DIR"
expect_rc 0

CASE="a pattern inside a -- comment line is ignored"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql '-- DROP TABLE widgets is not actually run here'
run_case "$DIR"
expect_rc 0
expect_no_out "needs expand-contract"

CASE="CREATE TABLE ... NOT NULL and DROP INDEX are allowed"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'CREATE TABLE widgets (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL); DROP INDEX IF EXISTS idx_old;'
run_case "$DIR"
expect_rc 0

CASE="a missing migrations directory fails loudly"
run_case "$WORK/does-not-exist"
expect_rc 1
expect_out "no such directory"

CASE="ADD COLUMN NOT NULL fails when the only DEFAULT belongs to a different ADD COLUMN clause"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'ALTER TABLE widgets ADD COLUMN weight INT NOT NULL, ADD COLUMN height INT DEFAULT 0;'
run_case "$DIR"
expect_rc 1
expect_out "ADD COLUMN ... NOT NULL without DEFAULT needs expand-contract"

CASE="ADD COLUMN NOT NULL fails when a DEFAULT clause precedes it on the same line"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'ALTER TABLE widgets ADD COLUMN height INT DEFAULT 0, ADD COLUMN weight INT NOT NULL;'
run_case "$DIR"
expect_rc 1
expect_out "ADD COLUMN ... NOT NULL without DEFAULT needs expand-contract"

CASE="ADD COLUMN NOT NULL fails when a DEFAULT sits in an earlier statement on the same line"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'ALTER TABLE a ADD COLUMN x INT DEFAULT 0; ALTER TABLE b ADD COLUMN y INT NOT NULL;'
run_case "$DIR"
expect_rc 1
expect_out "ADD COLUMN ... NOT NULL without DEFAULT needs expand-contract"

CASE="a DEFAULT written only in a trailing -- comment does not make ADD COLUMN NOT NULL safe"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'ALTER TABLE widgets ADD COLUMN weight INT NOT NULL; -- DEFAULT 0 later'
run_case "$DIR"
expect_rc 1
expect_out "ADD COLUMN ... NOT NULL without DEFAULT needs expand-contract"

CASE="ADD COLUMN with DEFAULT written before NOT NULL passes"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'ALTER TABLE widgets ADD COLUMN weight INT DEFAULT 0 NOT NULL;'
run_case "$DIR"
expect_rc 0

CASE="two-digit-leading versions such as 080, 089 and 099 are checked"
for v in 080 089 099; do
    DIR=$(mktemp -d -p "$WORK")
    write_fixture "$DIR" "${v}_x.sql" 'DROP TABLE widgets;'
    run_case "$DIR"
    expect_rc 1
    expect_out "${v}_x.sql: DROP TABLE needs expand-contract"
done

CASE="an unsafe statement after a safe one on the same line fails"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'CREATE TABLE a (id INT); ALTER TABLE widgets DROP COLUMN legacy;'
run_case "$DIR"
expect_rc 1
expect_out "DROP COLUMN needs expand-contract"

CASE="a -- contract comment trailing a SQL line does not exempt the file"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'DROP TABLE widgets; -- contract'
run_case "$DIR"
expect_rc 1
expect_out "DROP TABLE needs expand-contract"

CASE="a comment line that merely starts with contract does not exempt the file"
DIR=$(mktemp -d -p "$WORK")
printf '%s\n%s\n' 'DROP TABLE widgets;' '-- contractor table is gone' >"$DIR/026_x.sql"
run_case "$DIR"
expect_rc 1
expect_out "DROP TABLE needs expand-contract"

CASE="the contract marker is honoured with no space after -- and with trailing spaces"
DIR=$(mktemp -d -p "$WORK")
printf '%s\n%s\n' 'DROP TABLE widgets;' '--contract' >"$DIR/026_x.sql"
printf '%s\n%s\n' 'DROP TABLE gadgets;' '-- contract   ' >"$DIR/027_y.sql"
run_case "$DIR"
expect_rc 0

CASE="an unsafe statement followed by a -- comment on the same line still fails"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'ALTER TABLE widgets DROP COLUMN legacy; -- cleanup'
run_case "$DIR"
expect_rc 1
expect_out "DROP COLUMN needs expand-contract"

CASE="a pattern inside a trailing -- comment after safe SQL is ignored"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'SELECT 1; -- DROP TABLE widgets'
run_case "$DIR"
expect_rc 0
expect_no_out "needs expand-contract"

CASE="a CRLF file with an unsafe statement fails"
DIR=$(mktemp -d -p "$WORK")
printf 'DROP TABLE widgets;\r\n' >"$DIR/026_x.sql"
run_case "$DIR"
expect_rc 1
expect_out "DROP TABLE needs expand-contract"

CASE="a CRLF -- contract line exempts the file"
DIR=$(mktemp -d -p "$WORK")
printf 'DROP TABLE widgets;\r\n-- contract\r\n' >"$DIR/026_x.sql"
run_case "$DIR"
expect_rc 0

CASE="a pattern inside a CRLF -- comment line is ignored"
DIR=$(mktemp -d -p "$WORK")
printf -- '-- DROP TABLE widgets\r\nSELECT 1;\r\n' >"$DIR/026_x.sql"
run_case "$DIR"
expect_rc 0

CASE="lower and mixed case unsafe patterns fail"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'drop table widgets;'
write_fixture "$DIR" 027_x.sql 'Alter Table widgets Add Column weight Int Not Null;'
write_fixture "$DIR" 028_x.sql 'alter table widgets alter column name set not null;'
run_case "$DIR"
expect_rc 1
expect_out "026_x.sql: DROP TABLE needs expand-contract"
expect_out "027_x.sql: ADD COLUMN ... NOT NULL without DEFAULT needs expand-contract"
expect_out "028_x.sql: SET NOT NULL needs expand-contract"

CASE="tabs, repeated spaces and indentation between keywords still fail"
DIR=$(mktemp -d -p "$WORK")
printf 'DROP\tTABLE widgets;\n' >"$DIR/026_x.sql"
write_fixture "$DIR" 027_x.sql '    ALTER TABLE widgets DROP   COLUMN legacy;'
write_fixture "$DIR" 028_x.sql 'ALTER TABLE widgets ALTER COLUMN name SET  NOT  NULL;'
run_case "$DIR"
expect_rc 1
expect_out "026_x.sql: DROP TABLE needs expand-contract"
expect_out "027_x.sql: DROP COLUMN needs expand-contract"
expect_out "028_x.sql: SET NOT NULL needs expand-contract"

CASE="each hit prints the documented line naming the file and pattern"
DIR=$(mktemp -d -p "$WORK")
printf '%s\n%s\n' 'DROP TABLE a;' 'ALTER TABLE b DROP COLUMN c;' >"$DIR/026_x.sql"
run_case "$DIR"
expect_rc 1
expect_out "migration-safety: $DIR/026_x.sql: DROP TABLE needs expand-contract (mark the file -- contract once no running code needs the old shape)"
expect_out "migration-safety: $DIR/026_x.sql: DROP COLUMN needs expand-contract (mark the file -- contract once no running code needs the old shape)"

CASE="a contract marker in one file does not exempt an unmarked sibling"
DIR=$(mktemp -d -p "$WORK")
printf '%s\n%s\n' 'DROP TABLE widgets;' '-- contract' >"$DIR/026_x.sql"
write_fixture "$DIR" 027_y.sql 'ALTER TABLE gadgets RENAME TO things;'
run_case "$DIR"
expect_rc 1
expect_out "027_y.sql: RENAME needs expand-contract"
expect_no_out "026_x.sql"

CASE="with no argument the check scans ./migrations"
DIR=$(mktemp -d -p "$WORK")
mkdir "$DIR/migrations"
write_fixture "$DIR/migrations" 026_x.sql 'DROP TABLE widgets;'
OUT=$(cd "$DIR" && bash "$HERE/migration-safety.sh" 2>&1)
RC=$?
expect_rc 1
expect_out "026_x.sql: DROP TABLE needs expand-contract"

CASE="an empty migrations directory is clean"
DIR=$(mktemp -d -p "$WORK")
run_case "$DIR"
expect_rc 0

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all migration safety checks passed\n'
