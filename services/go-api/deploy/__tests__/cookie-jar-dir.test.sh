#!/usr/bin/env bash


set -uo pipefail

HERE=$(cd "$(dirname "$0")/.." && pwd)
parent=$(mktemp -d)
trap 'rm -rf "$parent"' EXIT
FAILURES=0
SECRET=$'# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t1999999999\tLOGIN_INFO\tnever-print-me'
FRESH=$'# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t1999999999\tLOGIN_INFO\tnever-print-me-either'
RUN="docker run --rm --pull never --network none"

setup_checkout() {
    WORK=$(mktemp -d -p "$parent")
    CHECKOUT="$WORK/altune"
    DIR="$CHECKOUT/cookies"
    mkdir -p "$WORK/bin" "$CHECKOUT/services/go-api/deploy"
    cp "$HERE/cookie-jar-dir.sh" "$CHECKOUT/services/go-api/deploy/"
    cat >"$WORK/bin/docker" <<'EOF'
#!/usr/bin/env bash
echo "docker $*" >>"$STUB_LOG"
if [ "$1 $2" = "image inspect" ]; then
    case " ${STUB_IMAGES-altune-go-api:blue} " in *" $3 "*) exit 0 ;; esac
    exit 1
fi
shift
as_root=0 mount="" entry=""
while [ $# -gt 0 ]; do
    case $1 in
        --rm) ;;
        --pull | --network) shift ;;
        -u) as_root=1; shift ;;
        -v) mount=${2%%:*}; shift ;;
        --entrypoint) entry=$2; shift ;;
        *) break ;;
    esac
    shift
done
shift
if [ -n "$mount" ]; then
    mapped=()
    for arg in "$@"; do mapped+=("$(printf '%s' "$arg" | sed -E "s#/c\b#$mount#g")"); done
    set -- "${mapped[@]}"
fi
case $entry in
    id) echo "${STUB_GID-$(id -g)}" ;;
    timeout)
        [ -z "${STUB_LOCK_WAIT-}" ] || set -- "$STUB_LOCK_WAIT" "${@:2}"
        exec timeout "$@"
        ;;
    sh)
        if [ $as_root = 1 ]; then
            [ -z "${STUB_FIX_RC-}" ] || exit "$STUB_FIX_RC"
        elif [ -z "$mount" ] && [ -n "${STUB_NO_FLOCK-}" ]; then
            exec env PATH=/nonexistent /bin/sh "$@"
        else
            [ -z "${STUB_CHECK_RC-}" ] || exit "$STUB_CHECK_RC"
        fi
        exec sh "$@"
        ;;
    *) exit 99 ;;
esac
EOF
    chmod +x "$WORK/bin/docker"
    : >"$WORK/actions.log"
}

run_script() {
    (cd "$CHECKOUT/services/go-api" && umask "${UMASK:-$(umask)}" && STUB_LOG="$WORK/actions.log" PATH="$WORK/bin:$PATH" bash deploy/cookie-jar-dir.sh "$@" >"$WORK/out.log" 2>&1)
    RC=$?
}

fail() {
    printf 'FAIL: %s\n      %s\n' "$CASE" "$1"
    FAILURES=$((FAILURES + 1))
}

expect_rc() {
    [ "$RC" = "$1" ] || fail "expected exit $1, got $RC ($(tail -1 "$WORK/out.log"))"
}

expect_out() {
    grep -qF -- "$1" "$WORK/out.log" || fail "expected output to mention '$1', got: $(cat "$WORK/out.log")"
}

expect_jar() {
    [ "$(cat "$DIR/cookies.txt" 2>/dev/null)" = "$1" ] || fail "expected the jar at cookies/cookies.txt to hold the ${2:-old} cookies"
}

expect_prev() {
    [ "$(cat "$DIR/cookies.txt.prev" 2>/dev/null)" = "$1" ] || fail "expected cookies.txt.prev to hold the ${2:-old} cookies"
    [ "$(stat -c %a "$DIR/cookies.txt.prev" 2>/dev/null)" = 660 ] || fail "expected cookies.txt.prev at 0660"
}

expect_no() {
    for name in "$@"; do
        [ -e "$DIR/$name" ] && fail "expected no $name"
    done
}

expect_untouched() {
    expect_jar "$SECRET"
    expect_no cookies.txt.new cookies.txt.prev
}

expect_shared_with_goapi() {
    [ "$(stat -c %a "$DIR")" = 2770 ] || fail "expected the folder at 2770, got $(stat -c %a "$DIR")"
    [ "$(stat -c %a "$DIR/cookies.txt")" = 660 ] || fail "expected the jar at 0660, got $(stat -c %a "$DIR/cookies.txt")"
    [ "$(stat -c %u:%g "$DIR")" = "$(id -u):$(id -g)" ] || fail "expected the folder owned by the VM user and go-api's gid, got $(stat -c %u:%g "$DIR")"
    [ "$(stat -c %g "$DIR/cookies.txt")" = "$(id -g)" ] || fail "expected the jar in go-api's group, got gid $(stat -c %g "$DIR/cookies.txt")"
}

expect_handed_over_in() {
    grep -qF "$RUN -u 0 --entrypoint sh -v $DIR:/c $1 -c " "$WORK/actions.log" ||
        fail "expected the folder handed over by root in $1 without a pull, got: $(cat "$WORK/actions.log")"
    grep -qxF "$RUN --entrypoint id $1 -g" "$WORK/actions.log" || fail "expected go-api's gid read from $1"
}

expect_swapped_under_goapis_lock() {
    grep -qF "$RUN --entrypoint timeout -v $DIR:/c altune-go-api:blue 30 flock /c/cookies.txt.lock sh -c " "$WORK/actions.log" ||
        fail "expected the swap run as go-api's user under /c/cookies.txt.lock, got: $(cat "$WORK/actions.log")"
}

expect_checked_in() {
    grep -qxF "$RUN --entrypoint sh -v $DIR:/c $1 -c test -r /c/cookies.txt && test -w /c && test -x /c" "$WORK/actions.log" ||
        fail "expected go-api's access checked as its own user in $1, got: $(cat "$WORK/actions.log")"
}

expect_no_docker_call() {
    [ -s "$WORK/actions.log" ] && fail "expected no docker call, got: $(cat "$WORK/actions.log")"
}

expect_no_docker_run() {
    grep -qF "docker run" "$WORK/actions.log" && fail "expected no container run, got: $(cat "$WORK/actions.log")"
}

expect_secret_never_printed() {
    grep -qF never-print-me "$WORK/out.log" "$WORK/actions.log" && fail "a jar's contents were printed"
}

expect_done() {
    expect_out "done: to drop in a new jar, run bash deploy/cookie-jar-dir.sh <export>, then delete the export; to undo the last one, run bash deploy/cookie-jar-dir.sh --rollback"
}

expect_not_done() {
    grep -qF "done:" "$WORK/out.log" && fail "expected no done line"
}

live_jar() {
    mkdir -p "$DIR"
    printf '%s' "$SECRET" >"$DIR/cookies.txt"
}

export_at() {
    printf '%s' "${2-$FRESH}" >"$1"
    chmod 0600 "$1"
}

CASE="a jar at the old path moves into the folder, shared with go-api's group, and nothing is left at the old path"
setup_checkout
printf '%s' "$SECRET" >"$CHECKOUT/cookies.txt"
chmod 0666 "$CHECKOUT/cookies.txt"
run_script
expect_rc 0
expect_jar "$SECRET"
[ -e "$CHECKOUT/cookies.txt" ] || [ -L "$CHECKOUT/cookies.txt" ] && fail "expected nothing left at the old path"
expect_out "stop dropping jars there"
expect_shared_with_goapi
expect_handed_over_in altune-go-api:blue
expect_checked_in altune-go-api:blue
expect_no cookies.txt.prev
expect_done
expect_secret_never_printed

CASE="a rerun after the move changes nothing and succeeds"
: >"$WORK/actions.log"
run_script
expect_rc 0
expect_jar "$SECRET"
expect_shared_with_goapi
expect_checked_in altune-go-api:blue

CASE="a jar already in the folder is handed to go-api's group and checked"
setup_checkout
mkdir -m 0755 "$DIR"
live_jar
chmod 0644 "$DIR/cookies.txt"
run_script
expect_rc 0
[ -e "$CHECKOUT/cookies.txt" ] && fail "expected no file created at the old path"
expect_shared_with_goapi
expect_checked_in altune-go-api:blue

CASE="a symlink at the old path is reported plainly and left alone"
setup_checkout
live_jar
ln -s cookies/cookies.txt "$CHECKOUT/cookies.txt"
run_script
expect_rc 0
expect_out "$CHECKOUT/cookies.txt is a symlink to cookies/cookies.txt, not a jar; nothing to move"
[ "$(readlink "$CHECKOUT/cookies.txt")" = cookies/cookies.txt ] || fail "expected the symlink left in place"
expect_jar "$SECRET"
expect_shared_with_goapi
expect_secret_never_printed

CASE="a file at the old path beside a migrated jar is refused, with the command that installs it"
setup_checkout
live_jar
printf 'other' >"$CHECKOUT/cookies.txt"
run_script
expect_rc 1
expect_out "both $CHECKOUT/cookies.txt and $DIR/cookies.txt exist; if $CHECKOUT/cookies.txt is the newer export, install it with: bash deploy/cookie-jar-dir.sh $CHECKOUT/cookies.txt; otherwise delete $CHECKOUT/cookies.txt"
[ "$(cat "$CHECKOUT/cookies.txt")" = other ] || fail "expected the old file left in place"
expect_untouched
expect_no_docker_call
expect_secret_never_printed

CASE="the old path given as the export beside a migrated jar is installed, kept as the jar's successor and removed"
setup_checkout
live_jar
export_at "$CHECKOUT/cookies.txt"
run_script "$CHECKOUT/cookies.txt"
expect_rc 0
expect_jar "$FRESH" new
expect_prev "$SECRET"
[ -e "$CHECKOUT/cookies.txt" ] && fail "expected the old path removed after the install"
expect_out "removed $CHECKOUT/cookies.txt: go-api never reads that path"
expect_done

CASE="the old path given as the export with no jar yet is installed and the script succeeds"
setup_checkout
export_at "$CHECKOUT/cookies.txt"
run_script "$CHECKOUT/cookies.txt"
expect_rc 0
expect_jar "$FRESH" new
expect_no cookies.txt.prev cookies.txt.new
[ -e "$CHECKOUT/cookies.txt" ] && fail "expected the old path removed after the install"
expect_shared_with_goapi
expect_done

CASE="no jar anywhere fails and says how to install one"
setup_checkout
run_script
expect_rc 1
expect_out "no cookie jar at $DIR/cookies.txt; install the browser export with: bash deploy/cookie-jar-dir.sh <export>"
expect_no_docker_call

CASE="an empty folder fails without its modes being touched"
setup_checkout
mkdir -m 0755 "$DIR"
run_script
expect_rc 1
expect_out "no cookie jar at $DIR/cookies.txt"
[ "$(stat -c %a "$DIR")" = 755 ] || fail "expected the folder left at 755, got $(stat -c %a "$DIR")"
expect_no_docker_call

CASE="a private export dropped in under umask 077 replaces the jar by rename under go-api's lock and keeps the old one as .prev"
setup_checkout
live_jar
export_at "$WORK/export.txt"
inode=$(stat -c %i "$DIR/cookies.txt")
UMASK=077 run_script "$WORK/export.txt"
expect_rc 0
expect_jar "$FRESH" new
[ "$(stat -c %i "$DIR/cookies.txt")" != "$inode" ] || fail "expected the jar replaced by rename, not written in place"
expect_prev "$SECRET"
expect_shared_with_goapi
expect_no cookies.txt.new cookies.txt.old
[ "$(cat "$WORK/export.txt")" = "$FRESH" ] || fail "expected the export left where it was"
expect_swapped_under_goapis_lock
expect_out "installed the export as $DIR/cookies.txt at 0660 in go-api's group, under go-api's jar lock; the jar it replaced is $DIR/cookies.txt.prev"
expect_checked_in altune-go-api:blue
expect_done
expect_secret_never_printed

CASE="a rollback swaps .prev back in and keeps the undone jar as .prev"
run_script --rollback
expect_rc 0
expect_jar "$SECRET"
expect_prev "$FRESH" new
expect_shared_with_goapi
expect_swapped_under_goapis_lock
expect_out "rolled back: the jar that was $DIR/cookies.txt.prev is live again, and the jar it replaced is now $DIR/cookies.txt.prev"
expect_done

CASE="a second rollback swaps them back"
run_script --rollback
expect_rc 0
expect_jar "$FRESH" new
expect_prev "$SECRET"

CASE="a rollback with no .prev is refused before anything is touched"
setup_checkout
live_jar
run_script --rollback
expect_rc 1
expect_out "no previous jar at $DIR/cookies.txt.prev to roll back to"
expect_untouched
expect_no_docker_call

CASE="an empty export is refused before anything is touched"
setup_checkout
live_jar
export_at "$WORK/export.txt" ""
run_script "$WORK/export.txt"
expect_rc 1
expect_out "$WORK/export.txt is not a cookies.txt export: its first line must be '# Netscape HTTP Cookie File'"
expect_untouched
expect_no_docker_call

CASE="an export without the Netscape header is refused before anything is touched"
setup_checkout
live_jar
export_at "$WORK/export.txt" $'.youtube.com\tTRUE\t/\tTRUE\t1999999999\tLOGIN_INFO\tnever-print-me'
run_script "$WORK/export.txt"
expect_rc 1
expect_out "is not a cookies.txt export"
expect_untouched
expect_no_docker_call
expect_secret_never_printed

CASE="an export whose header lacks the leading hash is refused before anything is touched"
setup_checkout
live_jar
export_at "$WORK/export.txt" $'Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t1999999999\tLOGIN_INFO\tnever-print-me'
run_script "$WORK/export.txt"
expect_rc 1
expect_out "is not a cookies.txt export: its first line must be"
expect_untouched
expect_no_docker_call

CASE="an export whose header is not on its first line is refused before anything is touched"
setup_checkout
live_jar
export_at "$WORK/export.txt" $'\n# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t1999999999\tLOGIN_INFO\tnever-print-me'
run_script "$WORK/export.txt"
expect_rc 1
expect_out "is not a cookies.txt export: its first line must be"
expect_untouched
expect_no_docker_call

for signed_out in \
    $'# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t1999999999\tSID\tnever-print-me' \
    $'# Netscape HTTP Cookie File\n.google.com\tTRUE\t/\tTRUE\t1999999999\tLOGIN_INFO\tnever-print-me' \
    $'# Netscape HTTP Cookie File\n.notyoutube.com\tTRUE\t/\tTRUE\t1999999999\tLOGIN_INFO\tnever-print-me' \
    $'# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t1999999999\tLOGIN_INFO'; do
    CASE="a signed-out export is refused before anything is touched: $(printf '%s' "$signed_out" | tail -1 | cut -f1,6 | tr '\t' ' ')"
    setup_checkout
    live_jar
    export_at "$WORK/export.txt" "$signed_out"
    run_script "$WORK/export.txt"
    expect_rc 1
    expect_out "$WORK/export.txt has no youtube.com LOGIN_INFO cookie, so it is signed out of YouTube; sign in to YouTube in that browser and export again"
    expect_untouched
    expect_no_docker_call
    expect_secret_never_printed
done

for signed_in in \
    $'# HTTP Cookie File\n#HttpOnly_.youtube.com\tTRUE\t/\tTRUE\t1999999999\tLOGIN_INFO\tl' \
    $'# Netscape HTTP Cookie File\r\nwww.youtube.com\tFALSE\t/\tTRUE\t1999999999\tLOGIN_INFO\tl\r\n.youtube.com\tTRUE\t/\tTRUE\t1999999999\tSIDCC\ts' \
    $'# Netscape HTTP Cookie File\nyoutube.com\tFALSE\t/\tTRUE\t1999999999\tLOGIN_INFO\tl'; do
    CASE="a signed-in export in another accepted form is installed: $(printf '%s' "$signed_in" | head -1 | tr -d '\r')"
    setup_checkout
    live_jar
    export_at "$WORK/export.txt" "$signed_in"
    run_script "$WORK/export.txt"
    expect_rc 0
    expect_jar "$signed_in" new
done

CASE="the live jar given as the export is refused"
setup_checkout
live_jar
run_script "$DIR/cookies.txt"
expect_rc 1
expect_out "$DIR/cookies.txt is the live jar already; nothing to install"
expect_untouched
expect_no_docker_call

CASE="a drop-in gives up, leaving the jar as it was, while go-api holds the jar lock"
setup_checkout
live_jar
export_at "$WORK/export.txt"
flock "$DIR/cookies.txt.lock" sleep 60 &
holder=$!
for _ in $(seq 500); do
    flock -n "$DIR/cookies.txt.lock" true || break
    sleep 0.01
done
flock -n "$DIR/cookies.txt.lock" true && fail "the test could not take the jar lock to hold it"
STUB_LOCK_WAIT=1 run_script "$WORK/export.txt"
kill "$holder" 2>/dev/null
wait "$holder" 2>/dev/null
expect_rc 1
expect_out "could not swap in the export under go-api's jar lock within 30s; the live jar is unchanged, so rerun"
expect_untouched
expect_not_done

CASE="a read-only cookies.txt.new left by an interrupted drop-in does not block the next one"
setup_checkout
live_jar
printf 'stale' >"$DIR/cookies.txt.new"
chmod 0400 "$DIR/cookies.txt.new"
export_at "$WORK/export.txt"
run_script "$WORK/export.txt"
expect_rc 0
expect_jar "$FRESH" new
expect_no cookies.txt.new

CASE="the handover uses the gid read from go-api's image, not the VM user's"
setup_checkout
live_jar
STUB_GID=4242 STUB_FIX_RC=0 run_script
expect_rc 0
grep -qF -- "-c chown $(id -u):4242 /c && chmod 2770 /c && if [ -e /c/cookies.txt ]; then chown $(id -u):4242 /c/cookies.txt && chmod 0660 /c/cookies.txt; fi" "$WORK/actions.log" ||
    fail "expected the folder and jar handed to $(id -u):4242, got: $(cat "$WORK/actions.log")"
expect_out "giving $DIR to uid $(id -u) and go-api's gid 4242 at 2770"

CASE="a relative export path is read from where the script was run"
setup_checkout
export_at "$WORK/export.txt"
RC=$(cd "$WORK" && STUB_LOG="$WORK/actions.log" PATH="$WORK/bin:$PATH" bash altune/services/go-api/deploy/cookie-jar-dir.sh export.txt >"$WORK/out.log" 2>&1; echo $?)
expect_rc 0
expect_jar "$FRESH" new
expect_shared_with_goapi

CASE="a missing export fails before anything is touched"
setup_checkout
live_jar
run_script "$WORK/absent.txt"
expect_rc 1
expect_out "no export at $WORK/absent.txt"
expect_untouched
expect_no_docker_call

CASE="more than one argument is a usage error"
setup_checkout
export_at "$WORK/export.txt"
run_script "$WORK/export.txt" "$WORK/export.txt"
expect_rc 2
expect_out "usage: bash deploy/cookie-jar-dir.sh [<new cookies export> | --rollback]"

CASE="an unknown flag is a usage error"
setup_checkout
run_script --roll-back
expect_rc 2
expect_out "usage: bash deploy/cookie-jar-dir.sh"

CASE="with only staging's image on the host the handover and check run in it"
setup_checkout
printf '%s' "$SECRET" >"$CHECKOUT/cookies.txt"
STUB_IMAGES=altune-staging-go-api:green run_script
expect_rc 0
expect_handed_over_in altune-staging-go-api:green
expect_checked_in altune-staging-go-api:green

CASE="with no go-api image on the host the script fails, before anything is touched, instead of pulling one"
setup_checkout
printf '%s' "$SECRET" >"$CHECKOUT/cookies.txt"
STUB_IMAGES="" run_script
expect_rc 1
expect_out "none of altune-go-api:blue altune-go-api:green altune-staging-go-api:blue altune-staging-go-api:green is on this host"
[ "$(cat "$CHECKOUT/cookies.txt")" = "$SECRET" ] || fail "expected the old jar left at the old path"
[ -e "$DIR" ] && fail "expected no folder made"
expect_no_docker_run
expect_not_done

CASE="an image without flock refuses a drop-in before anything is touched"
setup_checkout
live_jar
export_at "$WORK/export.txt"
STUB_NO_FLOCK=1 run_script "$WORK/export.txt"
expect_rc 1
expect_out "altune-go-api:blue has no flock or timeout, so the jar cannot be swapped under go-api's jar lock"
expect_untouched
grep -qF -- "-u 0" "$WORK/actions.log" && fail "expected no root handover"
expect_not_done

CASE="a gid that cannot be read from the image fails before anything is touched"
setup_checkout
live_jar
export_at "$WORK/export.txt"
STUB_GID="no such user" run_script "$WORK/export.txt"
expect_rc 1
expect_out "could not read go-api's gid from altune-go-api:blue"
grep -qF -- "-u 0" "$WORK/actions.log" && fail "expected no root handover"
expect_untouched
expect_not_done

CASE="a failed handover fails the script and leaves the jar as it was"
setup_checkout
live_jar
export_at "$WORK/export.txt"
STUB_FIX_RC=1 run_script "$WORK/export.txt"
[ "$RC" = 0 ] && fail "expected a failure"
expect_untouched
expect_not_done
expect_secret_never_printed

CASE="a jar go-api's user cannot use fails the script and says what the folder should be"
setup_checkout
printf '%s' "$SECRET" >"$CHECKOUT/cookies.txt"
STUB_CHECK_RC=1 run_script
expect_rc 1
expect_out "is uid $(id -u) gid $(id -g) mode 2770, want gid $(id -g) mode 2770"
expect_not_done
expect_secret_never_printed

if [ "$FAILURES" -gt 0 ]; then
    printf '%d failure(s)\n' "$FAILURES"
    exit 1
fi
echo "cookie-jar-dir: all cases passed"
