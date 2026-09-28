#!/usr/bin/env bash
set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
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
        PATH="$WORK/bin:$PATH" ALTUNE_HOST=stub ALTUNE_REMOTE_DIR="$WORK/checkout" \
            bash "$HERE/acq-debug.sh" "$@" >"$WORK/out.log" 2>"$WORK/err.log"
    )
    RC=$?
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
    PATH="$WORK/bin:$PATH" ALTUNE_HOST=stub ALTUNE_REMOTE_DIR="$missing_dir" \
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

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all acq-debug checks passed\n'
