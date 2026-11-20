#!/usr/bin/env bash


set -uo pipefail

HERE=$(cd "$(dirname "$0")/.." && pwd)
parent=$(mktemp -d)
trap 'rm -rf "$parent"' EXIT
FAILURES=0
SECRET='.youtube.com	TRUE	/	TRUE	1999999999	LOGIN_INFO	never-print-me'
FRESH='.youtube.com	TRUE	/	TRUE	1999999999	LOGIN_INFO	never-print-me-either'
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
if [ "$entry" = id ]; then
    echo "${STUB_GID-$(id -g)}"
    exit 0
fi
script=$(printf '%s' "$2" | sed -E "s#/c\b#$mount#g")
if [ $as_root = 1 ]; then
    [ -z "${STUB_FIX_RC-}" ] || exit "$STUB_FIX_RC"
else
    [ -z "${STUB_CHECK_RC-}" ] || exit "$STUB_CHECK_RC"
fi
sh -c "$script"
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
    grep -qF "$1" "$WORK/out.log" || fail "expected output to mention '$1', got: $(cat "$WORK/out.log")"
}

expect_jar() {
    [ "$(cat "$DIR/cookies.txt" 2>/dev/null)" = "$1" ] || fail "expected the jar at cookies/cookies.txt to hold the ${2:-old} cookies"
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

expect_checked_in() {
    grep -qxF "$RUN --entrypoint sh -v $DIR:/c $1 -c test -r /c/cookies.txt && test -w /c && test -x /c" "$WORK/actions.log" ||
        fail "expected go-api's access checked as its own user in $1, got: $(cat "$WORK/actions.log")"
}

expect_no_docker_run() {
    grep -qF "docker run" "$WORK/actions.log" && fail "expected no container run, got: $(cat "$WORK/actions.log")"
}

expect_secret_never_printed() {
    grep -qF never-print-me "$WORK/out.log" "$WORK/actions.log" && fail "a jar's contents were printed"
}

expect_done() {
    expect_out "done: to drop in a new jar, run bash deploy/cookie-jar-dir.sh <export>; never copy one in by hand"
}

expect_not_done() {
    grep -qF "done:" "$WORK/out.log" && fail "expected no done line"
}

export_at() {
    printf '%s' "$FRESH" >"$1"
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
printf '%s' "$SECRET" >"$DIR/cookies.txt"
chmod 0644 "$DIR/cookies.txt"
run_script
expect_rc 0
[ -e "$CHECKOUT/cookies.txt" ] && fail "expected no file created at the old path"
expect_shared_with_goapi
expect_checked_in altune-go-api:blue

CASE="a symlink at the old path is reported plainly and left alone"
setup_checkout
mkdir -p "$DIR"
printf '%s' "$SECRET" >"$DIR/cookies.txt"
ln -s cookies/cookies.txt "$CHECKOUT/cookies.txt"
run_script
expect_rc 0
expect_out "$CHECKOUT/cookies.txt is a symlink to cookies/cookies.txt, not a jar; nothing to move"
[ "$(readlink "$CHECKOUT/cookies.txt")" = cookies/cookies.txt ] || fail "expected the symlink left in place"
expect_jar "$SECRET"
expect_shared_with_goapi
expect_secret_never_printed

CASE="a jar at both paths is refused rather than guessing which is newer"
setup_checkout
mkdir -p "$DIR"
printf '%s' "$SECRET" >"$DIR/cookies.txt"
printf 'other' >"$CHECKOUT/cookies.txt"
run_script
expect_rc 1
expect_out "both $CHECKOUT/cookies.txt and $DIR/cookies.txt exist"
[ "$(cat "$CHECKOUT/cookies.txt")" = other ] || fail "expected the old file left in place"
[ -s "$WORK/actions.log" ] && fail "expected no docker call"
expect_secret_never_printed

CASE="no jar anywhere fails and says how to install one"
setup_checkout
run_script
expect_rc 1
expect_out "no cookie jar at $DIR/cookies.txt; rerun with the browser export: bash deploy/cookie-jar-dir.sh <export>"
[ -s "$WORK/actions.log" ] && fail "expected no docker call"

CASE="an empty folder fails without its modes being touched"
setup_checkout
mkdir -m 0755 "$DIR"
run_script
expect_rc 1
expect_out "no cookie jar at $DIR/cookies.txt"
[ "$(stat -c %a "$DIR")" = 755 ] || fail "expected the folder left at 755, got $(stat -c %a "$DIR")"
[ -s "$WORK/actions.log" ] && fail "expected no docker call"

CASE="a private export dropped in under umask 077 replaces the jar readable by go-api's group"
setup_checkout
mkdir -p "$DIR"
printf '%s' "$SECRET" >"$DIR/cookies.txt"
export_at "$WORK/export.txt"
inode=$(stat -c %i "$DIR/cookies.txt")
UMASK=077 run_script "$WORK/export.txt"
expect_rc 0
expect_jar "$FRESH" new
[ "$(stat -c %i "$DIR/cookies.txt")" != "$inode" ] || fail "expected the jar replaced by rename, not written in place"
expect_shared_with_goapi
[ -e "$DIR/cookies.txt.new" ] && fail "expected no cookies.txt.new left behind"
[ "$(cat "$WORK/export.txt")" = "$FRESH" ] || fail "expected the export left where it was"
expect_out "installed the export as $DIR/cookies.txt at 0660 in go-api's group"
expect_checked_in altune-go-api:blue
expect_done
expect_secret_never_printed

CASE="a read-only cookies.txt.new left by an interrupted drop-in does not block the next one"
setup_checkout
mkdir -p "$DIR"
printf '%s' "$SECRET" >"$DIR/cookies.txt"
printf 'stale' >"$DIR/cookies.txt.new"
chmod 0400 "$DIR/cookies.txt.new"
export_at "$WORK/export.txt"
run_script "$WORK/export.txt"
expect_rc 0
expect_jar "$FRESH" new
[ -e "$DIR/cookies.txt.new" ] && fail "expected no cookies.txt.new left behind"

CASE="the handover uses the gid read from go-api's image, not the VM user's"
setup_checkout
mkdir -p "$DIR"
printf '%s' "$SECRET" >"$DIR/cookies.txt"
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
mkdir -p "$DIR"
printf '%s' "$SECRET" >"$DIR/cookies.txt"
run_script "$WORK/absent.txt"
expect_rc 1
expect_out "no export at $WORK/absent.txt"
expect_jar "$SECRET"
[ -s "$WORK/actions.log" ] && fail "expected no docker call"

CASE="more than one argument is a usage error"
setup_checkout
export_at "$WORK/export.txt"
run_script "$WORK/export.txt" "$WORK/export.txt"
expect_rc 2
expect_out "usage: bash deploy/cookie-jar-dir.sh [<new cookies export>]"

CASE="with only staging's image on the host the handover and check run in it"
setup_checkout
printf '%s' "$SECRET" >"$CHECKOUT/cookies.txt"
STUB_IMAGES=altune-staging-go-api:green run_script
expect_rc 0
expect_handed_over_in altune-staging-go-api:green
expect_checked_in altune-staging-go-api:green

CASE="with no go-api image on the host the script fails instead of pulling one"
setup_checkout
printf '%s' "$SECRET" >"$CHECKOUT/cookies.txt"
STUB_IMAGES="" run_script
expect_rc 1
expect_out "none of altune-go-api:blue altune-go-api:green altune-staging-go-api:blue altune-staging-go-api:green is on this host"
expect_no_docker_run
expect_not_done

CASE="a gid that cannot be read from the image fails before any handover"
setup_checkout
mkdir -p "$DIR"
printf '%s' "$SECRET" >"$DIR/cookies.txt"
export_at "$WORK/export.txt"
STUB_GID="no such user" run_script "$WORK/export.txt"
expect_rc 1
expect_out "could not read go-api's gid from altune-go-api:blue"
grep -qF -- "-u 0" "$WORK/actions.log" && fail "expected no root handover"
expect_jar "$SECRET"
expect_not_done

CASE="a failed handover fails the script and leaves the jar as it was"
setup_checkout
mkdir -p "$DIR"
printf '%s' "$SECRET" >"$DIR/cookies.txt"
export_at "$WORK/export.txt"
STUB_FIX_RC=1 run_script "$WORK/export.txt"
[ "$RC" = 0 ] && fail "expected a failure"
expect_jar "$SECRET"
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
