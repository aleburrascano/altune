#!/usr/bin/env bash
set -uo pipefail

HERE=$(cd "$(dirname "$0")/.." && pwd)
FAILURES=0

new_stub_bin() {
    WORK=$(mktemp -d)
    mkdir -p "$WORK/bin"
    cat >"$WORK/bin/ssh" <<'STUBSSH'
#!/usr/bin/env bash
exec bash -c "${!#}"
STUBSSH
    chmod +x "$WORK/bin/ssh"
    cat >"$WORK/bin/docker" <<'STUBDOCKER'
#!/usr/bin/env bash
case $1 in
    ps) echo altune-go-api-x ;;
    *) : ;;
esac
STUBDOCKER
    chmod +x "$WORK/bin/docker"
    mkdir -p "$WORK/checkout/services/go-api"
    printf 'OTHER_VAR=1\n' >"$WORK/checkout/services/go-api/.env.production"
}

run_script() {
    (
        PATH="$WORK/bin:$PATH" HOME="${STUB_HOME:-$HOME}" ALTUNE_HOST=stub ALTUNE_REMOTE_DIR="$WORK/checkout" \
            bash "$HERE/acq-debug.sh" "$@" >"$WORK/out.log" 2>"$WORK/err.log"
    )
    RC=$?
}

stub_audit_tools() {
    cat >"$WORK/bin/psql" <<'STUBPSQL'
#!/usr/bin/env bash
sep=$(printf '\t')
while [ $# -gt 0 ]; do
    [ "$1" = -F ] && sep=$2
    shift
done
cat >/dev/null
printf 'id1%s%s%s%s%shttps://youtube.com/x\n' "$sep" "$ROW_TITLE" "$sep" "$ROW_ARTIST" "$sep"
STUBPSQL
    printf '#!/bin/sh\nexit 22\n' >"$WORK/bin/curl"
    printf '#!/bin/sh\nexit 0\n' >"$WORK/bin/sleep"
    printf '#!/bin/sh\nprintf "#!/bin/sh\\ncat\\n" > "$3"\nchmod +x "$3"\n' >"$WORK/bin/go"
    chmod +x "$WORK/bin/psql" "$WORK/bin/curl" "$WORK/bin/sleep" "$WORK/bin/go"
    printf 'DATABASE_URL=postgres://stub\n' >"$WORK/checkout/services/go-api/.env.production"
}

fail() {
    printf 'FAIL: %s\n      %s\n' "$CASE" "$1"
    FAILURES=$((FAILURES + 1))
}

expect_rc() {
    [ "$RC" = "$1" ] || fail "expected exit $1, got $RC"
}

expect_out() {
    grep -qF "$1" "$WORK/out.log" || fail "expected stdout to mention '$1'"
}

expect_err() {
    grep -qF "$1" "$WORK/err.log" || fail "expected stderr to mention '$1'"
}

expect_no_out() {
    [ ! -s "$WORK/out.log" ] || fail "expected empty stdout, got: $(cat "$WORK/out.log")"
}

expect_not_out() {
    grep -qF "$1" "$WORK/out.log" && fail "expected stdout not to mention '$1'"
    true
}

CASE="capture with no DATABASE_URL in the env file exits 3, reports the cause on stderr, prints nothing on stdout, and never falls through to 'no track matches'"
new_stub_bin
run_script capture foo
expect_rc 3
expect_err "acq-debug: no DATABASE_URL in .env.production"
expect_no_out
expect_not_out "no track matches"

CASE="capture reads the stopped blue/green colour too, so a candidate line logged before the last deploy reaches the dump"
new_stub_bin
stub_audit_tools
cat >"$WORK/bin/docker" <<'STUBDOCKER'
#!/usr/bin/env bash
case "$1 $2" in
    "ps -a") printf 'altune-go-api-green\naltune-go-api-blue\n' ;;
    ps*) echo altune-go-api-green ;;
    logs*)
        case "${!#}" in
            altune-go-api-blue) echo '{"time":"2026-10-16T04:29:14Z","msg":"candidate_evaluated","track_id":"03ae722b-52e2-4c42-a89b-5b7751020358","source":"ytdlp","candidate_title":"Smaxk Or Die"}' ;;
            altune-go-api-green) echo '{"time":"2026-10-16T05:00:00Z","msg":"redis connected"}' ;;
        esac ;;
esac
STUBDOCKER
cat >"$WORK/bin/psql" <<'STUBPSQL'
#!/usr/bin/env bash
if grep -q json_build_object; then echo '{"id":"03ae722b-52e2-4c42-a89b-5b7751020358"}'; else echo 03ae722b-52e2-4c42-a89b-5b7751020358; fi
STUBPSQL
chmod +x "$WORK/bin/docker" "$WORK/bin/psql"
run_script capture 03ae722b-52e2-4c42-a89b-5b7751020358
expect_rc 0
expect_out '"candidate_title":"Smaxk Or Die"'

CASE="capture reads ~/altune-logs archives, so a candidate line from before a container was recreated reaches the dump"
new_stub_bin
stub_audit_tools
mkdir -p "$WORK/home/altune-logs"
echo '{"time":"2026-09-20T04:29:14Z","msg":"candidate_evaluated","track_id":"03ae722b-52e2-4c42-a89b-5b7751020358","source":"ytdlp","candidate_title":"Only In The Archive"}' |
    gzip >"$WORK/home/altune-logs/go-api-green-20260920T050000Z.log.gz"
cat >"$WORK/bin/docker" <<'STUBDOCKER'
#!/usr/bin/env bash
case "$1 $2" in
    "ps -a") echo altune-go-api-green ;;
    ps*) echo altune-go-api-green ;;
esac
STUBDOCKER
cat >"$WORK/bin/psql" <<'STUBPSQL'
#!/usr/bin/env bash
if grep -q json_build_object; then echo '{"id":"03ae722b-52e2-4c42-a89b-5b7751020358"}'; else echo 03ae722b-52e2-4c42-a89b-5b7751020358; fi
STUBPSQL
chmod +x "$WORK/bin/docker" "$WORK/bin/psql"
STUB_HOME="$WORK/home" run_script capture 03ae722b-52e2-4c42-a89b-5b7751020358
expect_rc 0
expect_out '"candidate_title":"Only In The Archive"'

CASE="logs <since> keeps archived lines inside the window and drops older ones, and an unparseable window drops all archived lines"
new_stub_bin
mkdir -p "$WORK/home/altune-logs"
recent=$(date -u -d '10 minutes ago' +%Y-%m-%dT%H:%M:%SZ)
{
    echo '{"time":"2020-01-01T00:00:00Z","msg":"acquisition.audio_verdict","note":"too old"}'
    echo "{\"time\":\"$recent\",\"msg\":\"acquisition.audio_verdict\",\"note\":\"in window\"}"
} | gzip >"$WORK/home/altune-logs/go-api-green-20260920T050000Z.log.gz"
STUB_HOME="$WORK/home" run_script logs 1h
expect_rc 0
expect_out '"note":"in window"'
expect_not_out '"note":"too old"'
STUB_HOME="$WORK/home" run_script logs 99999h
expect_out '"note":"too old"'
STUB_HOME="$WORK/home" run_script logs nonsense
expect_not_out '"note":"in window"'
expect_not_out '"note":"too old"'

CASE="logs full keeps a whole long line, the default width cuts it at 400, and a bad width exits 3"
new_stub_bin
mkdir -p "$WORK/home/altune-logs"
recent=$(date -u -d '10 minutes ago' +%Y-%m-%dT%H:%M:%SZ)
pad=$(printf 'x%.0s' $(seq 1 500))
echo "{\"time\":\"$recent\",\"msg\":\"acquisition.audio_verdict\",\"pad\":\"$pad\",\"results\":\"tail-marker\"}" |
    gzip >"$WORK/home/altune-logs/go-api-green-20260920T050000Z.log.gz"
STUB_HOME="$WORK/home" run_script logs 1h . full
expect_rc 0
expect_out '"results":"tail-marker"'
STUB_HOME="$WORK/home" run_script logs 1h
expect_rc 0
expect_not_out 'tail-marker'
STUB_HOME="$WORK/home" run_script logs 1h . wide
expect_rc 3
expect_err "acq-debug: logs width must be a number or 'full'"

CASE="track with no argument exits 3 with the usage on stderr and nothing on stdout"
new_stub_bin
run_script track
expect_rc 3
expect_err "usage: track <text|uuid>"
expect_no_out

CASE="probe with no argument exits 3 with the usage on stderr and nothing on stdout"
new_stub_bin
run_script probe
expect_rc 3
expect_err "usage: probe <artist title>"
expect_no_out

CASE="track with no DATABASE_URL in the env file exits 3, reports the cause on stderr, prints nothing on stdout, and never falls through to 'no track matches'"
new_stub_bin
run_script track foo
expect_rc 3
expect_err "acq-debug: no DATABASE_URL in .env.production"
expect_no_out
expect_not_out "no track matches"

CASE="track with no matching row exits 1 with 'no track matches' on stderr and nothing on stdout"
new_stub_bin
printf 'DATABASE_URL=postgres://stub\n' >"$WORK/checkout/services/go-api/.env.production"
run_script track foo
expect_rc 1
expect_err "no track matches 'foo'"
expect_no_out

CASE="sql with two statements exits 3 with the guard message on stderr and nothing on stdout"
new_stub_bin
run_script sql "select 1; select 2"
expect_rc 3
expect_err "acq-debug: sql takes one statement, no ';' or backslash"
expect_no_out

CASE="an unknown command exits 3 with the message on stderr and nothing on stdout"
new_stub_bin
run_script nosuchcmd
expect_rc 3
expect_err "acq-debug: unknown command 'nosuchcmd'"
expect_no_out

CASE="checkout missing on the VM exits 3 with the checkout message on stderr and nothing on stdout"
new_stub_bin
missing_dir="$WORK/checkout-missing"
(
    PATH="$WORK/bin:$PATH" HOME="${STUB_HOME:-$HOME}" ALTUNE_HOST=stub ALTUNE_REMOTE_DIR="$missing_dir" \
        bash "$HERE/acq-debug.sh" summary >"$WORK/out.log" 2>"$WORK/err.log"
)
RC=$?
expect_rc 3
expect_err "acq-debug: no checkout at $missing_dir on the VM"
expect_no_out

CASE="db() with no DATABASE_URL, reached directly (not through capture's pre-check), exits 3 with db()'s own message on stderr"
new_stub_bin
run_script summary
expect_rc 3
expect_err "acq-debug: no DATABASE_URL in .env.production"
expect_no_out

CASE="sql with a non-SELECT statement exits 3 with the second guard's message on stderr and nothing on stdout"
new_stub_bin
run_script sql "delete from tracks"
expect_rc 3
expect_err "acq-debug: sql only runs SELECT / WITH / EXPLAIN / SHOW / TABLE / VALUES"
expect_no_out

CASE="audit-qualifiers with an empty artist keeps the url in column 4"
new_stub_bin
stub_audit_tools
ROW_TITLE=T ROW_ARTIST= run_script audit-qualifiers
expect_rc 0
[ "$(cat "$WORK/out.log")" = "$(printf 'id1\tT\t\thttps://youtube.com/x\t')" ] || fail "columns shifted: $(cat "$WORK/out.log")"

CASE="audit-qualifiers with an empty title keeps the artist in column 3 and the url in column 4"
new_stub_bin
stub_audit_tools
ROW_TITLE= ROW_ARTIST=A run_script audit-qualifiers
expect_rc 0
[ "$(cat "$WORK/out.log")" = "$(printf 'id1\t\tA\thttps://youtube.com/x\t')" ] || fail "columns shifted: $(cat "$WORK/out.log")"

CASE="export-tracks with a non-numeric window exits 3 with the usage on stderr and nothing on stdout"
new_stub_bin
run_script export-tracks soon
expect_rc 3
expect_err "usage: export-tracks [days]"
expect_no_out

CASE="export-tracks prints the psql rows through the read-only session and passes the window"
new_stub_bin
printf 'DATABASE_URL=postgres://stub\n' >"$WORK/checkout/services/go-api/.env.production"
cat >"$WORK/bin/psql" <<'STUBPSQL'
#!/usr/bin/env bash
printf '%s\n' "$*" >"$WORK/psql.args"
cat >"$WORK/psql.sql"
printf '{"id":"t1"}\n'
STUBPSQL
chmod +x "$WORK/bin/psql"
WORK=$WORK run_script export-tracks 30
expect_rc 0
[ "$(cat "$WORK/out.log")" = '{"id":"t1"}' ] || fail "rows not passed through: $(cat "$WORK/out.log")"
grep -qF "days=30" "$WORK/psql.args" || fail "window not passed to psql: $(cat "$WORK/psql.args")"
[ "$(head -1 "$WORK/psql.sql")" = "BEGIN READ ONLY;" ] || fail "export-tracks query not in a read-only transaction"
grep -qiE '\b(insert|update|delete|drop)\b' "$WORK/psql.sql" && fail "export query must be read-only"

CASE="sql pipes its statement inside a read-only transaction with a local timeout and no session read-only SET"
new_stub_bin
printf 'DATABASE_URL=postgres://stub\n' >"$WORK/checkout/services/go-api/.env.production"
cat >"$WORK/bin/psql" <<'STUBPSQL'
#!/usr/bin/env bash
cat >"$WORK/psql.sql"
printf '1\n'
STUBPSQL
chmod +x "$WORK/bin/psql"
WORK=$WORK run_script sql "select 1"
expect_rc 0
[ "$(head -1 "$WORK/psql.sql")" = "BEGIN READ ONLY;" ] || fail "first line is not BEGIN READ ONLY: $(head -1 "$WORK/psql.sql")"
[ "$(tail -1 "$WORK/psql.sql")" = "COMMIT;" ] || fail "last line is not COMMIT: $(tail -1 "$WORK/psql.sql")"
grep -qF "SET LOCAL statement_timeout = '30s';" "$WORK/psql.sql" || fail "timeout is not transaction-local"
grep -qF "select 1;" "$WORK/psql.sql" || fail "statement missing from the piped sql"
grep -qF "default_transaction_read_only" "$WORK/psql.sql" && fail "session read-only SET must be gone"

if [ -n "${DATABASE_URL:-}" ]; then
    real_pg_sql() {
        new_stub_bin
        printf 'DATABASE_URL=%s\n' "$DATABASE_URL" >"$WORK/checkout/services/go-api/.env.production"
        PGOPTIONS='-c default_transaction_read_only=off' run_script sql "$1"
    }
    probe_rows() {
        psql "$DATABASE_URL" -X -q -A -t -c "SELECT count(*) FROM acq_debug_probe_rows"
    }
    psql "$DATABASE_URL" -X -q -c "DROP TABLE IF EXISTS acq_debug_probe_rows; CREATE TABLE acq_debug_probe_rows (id int)" >/dev/null 2>&1

    CASE="sql with a data-modifying CTE is refused by the database even when the session default is off"
    real_pg_sql "WITH d AS (INSERT INTO acq_debug_probe_rows VALUES (1) RETURNING 1) SELECT count(*) FROM d"
    [ "$RC" != 0 ] || fail "writing CTE exited 0"
    expect_err "read-only transaction"
    [ "$(probe_rows)" = 0 ] || fail "writing CTE inserted a row: $(probe_rows)"

    CASE="sql with a CTE that first switches transaction_read_only off still cannot write"
    real_pg_sql "WITH s AS (SELECT set_config('transaction_read_only', 'off', true)), d AS (INSERT INTO acq_debug_probe_rows SELECT 1 FROM s RETURNING 1) SELECT count(*) FROM d"
    [ "$RC" != 0 ] || fail "set_config then write exited 0"
    [ "$(probe_rows)" = 0 ] || fail "set_config then write inserted a row: $(probe_rows)"

    CASE="sql with two statements split by a newline instead of ';' writes nothing"
    real_pg_sql "$(printf 'SELECT 1\nINSERT INTO acq_debug_probe_rows VALUES (1)')"
    [ "$RC" != 0 ] || fail "newline-split statements exited 0"
    [ "$(probe_rows)" = 0 ] || fail "newline-split statements inserted a row: $(probe_rows)"

    CASE="sql with a SELECT through the real database prints its rows"
    real_pg_sql "SELECT 41 + 1"
    expect_rc 0
    expect_out 42

    psql "$DATABASE_URL" -X -q -c "DROP TABLE IF EXISTS acq_debug_probe_rows" >/dev/null
fi

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all acq-debug checks passed\n'
