#!/usr/bin/env bash


set -uo pipefail

HERE=$(cd "$(dirname "$0")/.." && pwd)
parent=$(mktemp -d)
trap 'rm -rf "$parent"' EXIT
FAILURES=0

DSN_SECRET="postgres://sweeper:hunter2-secret@db.internal:5432/altune"
RECONCILE_RUN_ID=0a1b2c3d-1111-4222-8333-444455556666
BIND_RUN_ID=9f8e7d6c-1111-4222-8333-444455556666

stub_tool() {
    local name=$1 dsn_var=$2 body=$3
    cat >"$WORK/tools/$name" <<EOF
#!/usr/bin/env bash
dsn_state=nodsn
[ "\${$dsn_var:-}" = '$DSN_SECRET' ] && dsn_state=dsn
echo "$name \$* [\$dsn_state]" >> "$WORK/actions.log"
$body
EOF
}

setup_case() {
    local last_success=${1:-}
    WORK=$(mktemp -d -p "$parent")
    mkdir -p "$WORK/bin" "$WORK/api/deploy" "$WORK/state" "$WORK/tools"
    cp "$HERE/prod_host.sh" "$HERE/library-sweep.sh" "$WORK/api/deploy/"
    : >"$WORK/actions.log"
    [ -z "$last_success" ] || printf '%s\n' "$last_success" >"$WORK/state/last-success"

    stub_tool librarybind LIBRARYBIND_DATABASE_URL '
report=""
while [ $# -gt 0 ]; do [ "$1" = -report ] && report=$2; shift; done
[ -z "$report" ] || printf "%s" "${STUB_LENGTH_PLAN:-}" >"$report"
echo "run '"$BIND_RUN_ID"' (apply): 3 tracks"
exit ${STUB_BIND_RC:-0}'
    stub_tool libraryderive LIBRARYDERIVE_DATABASE_URL '
report=""
while [ $# -gt 0 ]; do [ "$1" = -report ] && report=$2; shift; done
echo "{}" >"$report"
exit ${STUB_DERIVE_RC:-0}'
    stub_tool libraryreconcile RECONCILE_DATABASE_URL '
[ "${STUB_RECONCILE_NO_RUN_ID:-}" = 1 ] || echo "run_id '"$RECONCILE_RUN_ID"'"
exit ${STUB_RECONCILE_RC:-0}'
    printf '#!/usr/bin/env bash\nexit 0\n' >"$WORK/tools/retag"

    cat >"$WORK/bin/ssh" <<EOF
#!/usr/bin/env bash
remote_command=\$(cat)
case "\$remote_command" in
    *"grep -E '^DATABASE_URL='"*) printf '%s\n' '$DSN_SECRET' ;;
    *"docker run"*) echo "ssh docker run \$remote_command" >> "$WORK/actions.log"; exit \${STUB_RETAG_RC:-0} ;;
    *"uname -m"*) echo x86_64 ;;
esac
exit 0
EOF
    cat >"$WORK/bin/scp" <<EOF
#!/usr/bin/env bash
echo "scp \$*" >> "$WORK/actions.log"
exit 0
EOF
    cat >"$WORK/bin/hostname" <<EOF
#!/usr/bin/env bash
case "\$1" in
    -I) echo "10.0.0.5 \${STUB_LOCAL_IP:-192.168.1.20}" ;;
    -s) echo "\${STUB_HOSTNAME:-devbox}" ;;
    *)  echo "\${STUB_HOSTNAME:-devbox}.lan" ;;
esac
EOF
    cat >"$WORK/bin/getent" <<EOF
#!/usr/bin/env bash
echo "203.0.113.7 \$2"
EOF
    chmod +x "$WORK/bin"/* "$WORK/tools"/*
}

run_sweep() {
    (cd "$WORK/api" && PATH="$WORK/bin:$PATH" SWEEP_STATE_DIR="$WORK/state" SWEEP_BIN_DIR="$WORK/tools" \
        SWEEP_VM_KEY=/dev/null bash deploy/library-sweep.sh "$@" >"$WORK/out.log" 2>&1)
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
    grep -qF -- "$1" "$WORK/actions.log" || fail "expected action '$1'"
}

expect_no_action() {
    ! grep -qF -- "$1" "$WORK/actions.log" || fail "unexpected action '$1'"
}

expect_out() {
    grep -qF -- "$1" "$WORK/out.log" || fail "expected output to mention '$1'"
}

expect_last_success() {
    [ "$(cat "$WORK/state/last-success" 2>/dev/null)" = "$1" ] || fail "last-success is '$(cat "$WORK/state/last-success" 2>/dev/null)', expected '$1'"
}

expect_last_success_moved() {
    local now
    now=$(cat "$WORK/state/last-success" 2>/dev/null)
    { [ -n "$now" ] && [ "$now" != "$1" ]; } || fail "last-success did not move from '$1'"
}

OLD=2026-10-01T00:00:00Z

CASE="a sweep chains bind, derive, reconcile and retag, each with the prod DSN"
setup_case "$OLD"
run_sweep
expect_rc 0
expect_action "librarybind -apply -report"
expect_action "libraryderive -since $OLD -report"
expect_action "libraryreconcile -report"
expect_action "[dsn]"
expect_no_action "[nodsn]"
expect_last_success_moved "$OLD"

CASE="the run id reconcile printed is the run id retag is handed"
setup_case "$OLD"
run_sweep
expect_rc 0
expect_action "ssh docker run"
expect_action "-run $RECONCILE_RUN_ID -apply"
expect_action "scp"

CASE="the first sweep derives everything, with no -since"
setup_case
run_sweep
expect_rc 0
expect_action "libraryderive -report"
expect_no_action "libraryderive -since"
expect_last_success_moved ""

CASE="the DSN is never written under the state dir"
setup_case "$OLD"
run_sweep
expect_rc 0
if grep -rqF hunter2-secret "$WORK/state"; then fail "the state dir holds the DSN"; fi

CASE="every run is logged with its run ids"
setup_case "$OLD"
run_sweep
expect_rc 0
grep -qF "bind=$BIND_RUN_ID reconcile=$RECONCILE_RUN_ID" "$WORK/state/sweeps.log" || fail "sweeps.log lacks the run ids"
grep -qF "$RECONCILE_RUN_ID" "$WORK"/state/runs/*/sweep.log || fail "the run log lacks the run ids"

CASE="a second sweep while one holds the lock does nothing"
setup_case "$OLD"
flock "$WORK/state/lock" sleep 30 &
holder=$!
until [ -e "$WORK/state/lock" ]; do sleep 0.1; done
sleep 0.3
run_sweep
kill "$holder" 2>/dev/null
wait "$holder" 2>/dev/null
expect_rc 0
expect_out "SKIPPED: another sweep holds"
[ ! -s "$WORK/actions.log" ] || fail "a tool ran while the lock was held"
expect_last_success "$OLD"

CASE="a sweep right after a successful one is skipped"
setup_case "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
run_sweep
expect_rc 0
expect_out "SKIPPED: the last sweep succeeded"
[ ! -s "$WORK/actions.log" ] || fail "a tool ran inside the minimum interval"

CASE="a failed bind stops the sweep and leaves the sweep time put"
setup_case "$OLD"
STUB_BIND_RC=1 run_sweep
expect_rc 1
expect_no_action "libraryderive"
expect_last_success "$OLD"

CASE="a catalog-blocked derive stops before reconcile and leaves the sweep time put"
setup_case "$OLD"
STUB_DERIVE_RC=1 run_sweep
expect_rc 1
expect_action "librarybind"
expect_no_action "libraryreconcile"
expect_no_action "ssh docker run"
expect_last_success "$OLD"
grep -q 'failed(1)' "$WORK/state/sweeps.log" || fail "sweeps.log does not record the failure"

CASE="a failed reconcile leaves the sweep time put and retags nothing"
setup_case "$OLD"
STUB_RECONCILE_RC=1 run_sweep
expect_rc 1
expect_no_action "ssh docker run"
expect_last_success "$OLD"

CASE="a reconcile that prints no run id retags nothing and leaves the sweep time put"
setup_case "$OLD"
STUB_RECONCILE_NO_RUN_ID=1 run_sweep
expect_rc 1
expect_out "printed no run id"
expect_no_action "ssh docker run"
expect_last_success "$OLD"

CASE="a failed retag leaves the sweep time put so the next run retries"
setup_case "$OLD"
STUB_RETAG_RC=1 run_sweep
expect_rc 1
expect_last_success "$OLD"

CASE="a fix_isrc plan is logged and no tool applies it"
setup_case "$OLD"
STUB_LENGTH_PLAN='{"action":"fix_isrc","new_isrc":"USPR38007190"}' run_sweep
expect_rc 0
expect_out "logged and not applied"
expect_out "USPR38007190"
expect_no_action "libraryrelength"

CASE="a dry run prints each step and writes nothing"
setup_case "$OLD"
run_sweep --dry-run
expect_rc 0
expect_out "would: bind"
expect_out "would: derive: libraryderive -since $OLD"
expect_out "would: reconcile"
expect_out "would: retag on the VM"
expect_out "would: write"
[ ! -s "$WORK/actions.log" ] || fail "a dry run ran a tool or ssh"
[ ! -e "$WORK/state/runs" ] && [ ! -e "$WORK/state/sweeps.log" ] || fail "a dry run wrote to the state dir"
expect_last_success "$OLD"

CASE="the sweep refuses to run on a host named like the prod VM"
setup_case "$OLD"
STUB_HOSTNAME=altune run_sweep
expect_rc 1
expect_out "do not run the library sweep from the prod VM"
[ ! -s "$WORK/actions.log" ] || fail "a tool ran on the prod VM"

CASE="the sweep refuses to run on a host holding the prod VM's IP"
setup_case "$OLD"
STUB_LOCAL_IP=203.0.113.7 run_sweep
expect_rc 1
expect_out "do not run the library sweep from the prod VM"

CASE="an unknown argument is refused"
setup_case "$OLD"
run_sweep --apply
expect_rc 2

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all library sweep checks passed\n'
