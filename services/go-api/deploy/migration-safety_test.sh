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

CASE="ADD COLUMN NUMERIC(10,2) NOT NULL without DEFAULT fails"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'ALTER TABLE widgets ADD COLUMN price NUMERIC(10,2) NOT NULL;'
run_case "$DIR"
expect_rc 1
expect_out "ADD COLUMN ... NOT NULL without DEFAULT needs expand-contract"

CASE="ADD COLUMN NUMERIC(10,2) NOT NULL with a DEFAULT in the same clause passes"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'ALTER TABLE widgets ADD COLUMN price NUMERIC(10,2) NOT NULL DEFAULT 0;'
run_case "$DIR"
expect_rc 0

CASE="a safe ADD COLUMN clause followed by an unsafe NUMERIC(10,2) clause fails"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'ALTER TABLE widgets ADD COLUMN a INT, ADD COLUMN price NUMERIC(10,2) NOT NULL;'
run_case "$DIR"
expect_rc 1
expect_out "ADD COLUMN ... NOT NULL without DEFAULT needs expand-contract"

CASE="a CHECK clause containing a comma does not hide a later unsafe clause"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql "ALTER TABLE widgets ADD COLUMN price NUMERIC(10,2) CHECK (price IN (1,2)) NOT NULL;"
run_case "$DIR"
expect_rc 1
expect_out "ADD COLUMN ... NOT NULL without DEFAULT needs expand-contract"

CASE="a NNN_contract_<name>.sql filename exempts an otherwise unsafe file"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_contract_x.sql 'DROP TABLE IF EXISTS widgets;'
run_case "$DIR"
expect_rc 0

CASE="a -- contract comment line no longer exempts the file"
DIR=$(mktemp -d -p "$WORK")
printf '%s\n%s\n' 'DROP TABLE IF EXISTS widgets;' '-- contract' >"$DIR/026_x.sql"
run_case "$DIR"
expect_rc 1
expect_out "DROP TABLE needs expand-contract"

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

CASE="026_contracts_x.sql is still checked"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_contracts_x.sql 'DROP TABLE widgets;'
run_case "$DIR"
expect_rc 1
expect_out "026_contracts_x.sql: DROP TABLE needs expand-contract"

CASE="026_x_contract.sql is still checked"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x_contract.sql 'DROP TABLE widgets;'
run_case "$DIR"
expect_rc 1
expect_out "026_x_contract.sql: DROP TABLE needs expand-contract"

CASE="the _contract_ segment is matched case-sensitively"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_CONTRACT_x.sql 'DROP TABLE widgets;'
run_case "$DIR"
expect_rc 1
expect_out "026_CONTRACT_x.sql: DROP TABLE needs expand-contract"

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

CASE="a CRLF -- contract line no longer exempts the file"
DIR=$(mktemp -d -p "$WORK")
printf 'DROP TABLE widgets;\r\n-- contract\r\n' >"$DIR/026_x.sql"
run_case "$DIR"
expect_rc 1
expect_out "DROP TABLE needs expand-contract"

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
expect_out "migration-safety: $DIR/026_x.sql: DROP TABLE needs expand-contract (rename the file to NNN_contract_<name>.sql once no running code needs the old shape)"
expect_out "migration-safety: $DIR/026_x.sql: DROP COLUMN needs expand-contract (rename the file to NNN_contract_<name>.sql once no running code needs the old shape)"

CASE="a contract file does not exempt an unmarked sibling"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_contract_x.sql 'DROP TABLE widgets;'
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

CASE="ADD COLUMN NOT NULL split across lines fails"
DIR=$(mktemp -d -p "$WORK")
printf '%s\n%s\n' 'ALTER TABLE widgets ADD COLUMN weight INT' '    NOT NULL;' >"$DIR/026_x.sql"
run_case "$DIR"
expect_rc 1
expect_out "ADD COLUMN ... NOT NULL without DEFAULT needs expand-contract"

CASE="ADD COLUMN NOT NULL with its DEFAULT on the next line passes"
DIR=$(mktemp -d -p "$WORK")
printf '%s\n%s\n' 'ALTER TABLE widgets ADD COLUMN weight INT NOT NULL' '    DEFAULT 0;' >"$DIR/026_x.sql"
run_case "$DIR"
expect_rc 0

CASE="ADD COLUMN NOT NULL with its DEFAULT on the next CRLF line passes"
DIR=$(mktemp -d -p "$WORK")
printf 'ALTER TABLE widgets ADD COLUMN weight INT NOT NULL\r\n    DEFAULT 0;\r\n' >"$DIR/026_x.sql"
run_case "$DIR"
expect_rc 0

CASE="DROP and TABLE on separate lines fails"
DIR=$(mktemp -d -p "$WORK")
printf '%s\n%s\n' 'DROP' 'TABLE widgets;' >"$DIR/026_x.sql"
run_case "$DIR"
expect_rc 1
expect_out "DROP TABLE needs expand-contract"

CASE="a column named renamed_at passes even though it contains rename"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'ALTER TABLE widgets ADD COLUMN renamed_at TIMESTAMP NOT NULL DEFAULT now();'
run_case "$DIR"
expect_rc 0

CASE="a table named rename_log passes even though it contains rename"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'CREATE TABLE rename_log (id BIGSERIAL PRIMARY KEY);'
run_case "$DIR"
expect_rc 0

CASE="ALTER TYPE RENAME VALUE still fails"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql "ALTER TYPE mood RENAME VALUE 'sad' TO 'blue';"
run_case "$DIR"
expect_rc 1
expect_out "RENAME needs expand-contract"

CASE="a DROP-sounding phrase inside a string literal is ignored"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql "INSERT INTO logs (msg) VALUES ('we will drop table later');"
run_case "$DIR"
expect_rc 0

CASE="a -- inside a string literal does not swallow the statement that follows"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql "INSERT INTO logs (msg) VALUES ('a--b'); DROP TABLE w;"
run_case "$DIR"
expect_rc 1
expect_out "DROP TABLE needs expand-contract"

CASE="a pattern inside a block comment is ignored"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql '/* DROP TABLE w */ SELECT 1;'
run_case "$DIR"
expect_rc 0

CASE="a block comment containing -- does not hide the statement that follows"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql '/* -- */ DROP TABLE w;'
run_case "$DIR"
expect_rc 1
expect_out "DROP TABLE needs expand-contract"

CASE="TRUNCATE fails"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'TRUNCATE widgets;'
run_case "$DIR"
expect_rc 1
expect_out "TRUNCATE needs expand-contract"

CASE="ALTER COLUMN ... TYPE fails"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'ALTER TABLE widgets ALTER COLUMN name TYPE varchar(50);'
run_case "$DIR"
expect_rc 1
expect_out "ALTER COLUMN ... TYPE needs expand-contract"

CASE="ALTER COLUMN ... SET DATA TYPE fails"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'ALTER TABLE widgets ALTER COLUMN name SET DATA TYPE varchar(50);'
run_case "$DIR"
expect_rc 1
expect_out "ALTER COLUMN ... TYPE needs expand-contract"

CASE="DROP TYPE, DROP VIEW, DROP MATERIALIZED VIEW and DROP SCHEMA fail"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'DROP TYPE mood;'
write_fixture "$DIR" 027_x.sql 'DROP VIEW v;'
write_fixture "$DIR" 028_x.sql 'DROP MATERIALIZED VIEW mv;'
write_fixture "$DIR" 029_x.sql 'DROP SCHEMA s;'
run_case "$DIR"
expect_rc 1
expect_out "026_x.sql: DROP TYPE needs expand-contract"
expect_out "027_x.sql: DROP VIEW needs expand-contract"
expect_out "028_x.sql: DROP MATERIALIZED VIEW needs expand-contract"
expect_out "029_x.sql: DROP SCHEMA needs expand-contract"

CASE="ALTER TABLE ... DROP without the word COLUMN fails"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'ALTER TABLE widgets DROP legacy;'
run_case "$DIR"
expect_rc 1
expect_out "DROP COLUMN needs expand-contract"

CASE="ALTER TABLE ... ADD ... NOT NULL without the word COLUMN and without DEFAULT fails"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'ALTER TABLE widgets ADD weight INT NOT NULL;'
run_case "$DIR"
expect_rc 1
expect_out "ADD COLUMN ... NOT NULL without DEFAULT needs expand-contract"

CASE="DROP INDEX passes"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'DROP INDEX idx;'
run_case "$DIR"
expect_rc 0

CASE="ALTER TABLE ... DROP CONSTRAINT passes"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'ALTER TABLE widgets DROP CONSTRAINT c;'
run_case "$DIR"
expect_rc 0

CASE="ALTER TABLE ... ALTER COLUMN ... DROP DEFAULT passes"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'ALTER TABLE widgets ALTER COLUMN name DROP DEFAULT;'
run_case "$DIR"
expect_rc 0

CASE="a CHECK constraint passes"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 026_x.sql 'ALTER TABLE widgets ADD CONSTRAINT chk CHECK (age IS NOT NULL);'
run_case "$DIR"
expect_rc 0

CASE="a four-digit version such as 1000 is checked"
DIR=$(mktemp -d -p "$WORK")
write_fixture "$DIR" 1000_x.sql 'DROP TABLE widgets;'
run_case "$DIR"
expect_rc 1
expect_out "1000_x.sql: DROP TABLE needs expand-contract"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all migration safety checks passed\n'
