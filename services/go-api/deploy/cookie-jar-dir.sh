#!/usr/bin/env bash

set -euo pipefail

LOCK_WAIT_S=30
GOAPI_IMAGES=(altune-go-api:blue altune-go-api:green altune-staging-go-api:blue altune-staging-go-api:green)

log() {
    printf '[cookie-jar-dir] %s\n' "$*" >&2
}

fail() {
    log "FAILED: $*"
    exit 1
}

usage() {
    log "usage: bash deploy/cookie-jar-dir.sh [<new cookies export> | --rollback]"
    exit 2
}

in_image() {
    docker run --rm --pull never --network none "$@"
}

check_export() {
    local first="" header='^#( Netscape)? HTTP Cookie File'
    IFS= read -r first <"$1" || true
    [[ $first =~ $header ]] ||
        fail "$1 is not a cookies.txt export: its first line must be '# Netscape HTTP Cookie File'; export cookies.txt from the browser again"
    awk -F '\t' 'NF == 7 && $6 == "LOGIN_INFO" { sub(/^#HttpOnly_/, "", $1); sub(/^\./, "", $1); if ($1 == "youtube.com" || $1 == "www.youtube.com") found = 1 } END { exit !found }' "$1" ||
        fail "$1 has no youtube.com LOGIN_INFO cookie, so it is signed out of YouTube; sign in to YouTube in that browser and export again"
}

[ $# -le 1 ] || usage
MODE=setup
EXPORT=""
case "${1-}" in
    "") ;;
    --rollback) MODE=rollback ;;
    -*) usage ;;
    *)
        [ -f "$1" ] || fail "no export at $1"
        [ -r "$1" ] || fail "cannot read the export $1 as $(id -un)"
        MODE=install
        EXPORT=$(realpath -- "$1")
        ;;
esac

cd "$(dirname "$0")/.." || exit 1

CHECKOUT=$(cd ../.. && pwd -P)
OLD="$CHECKOUT/cookies.txt"
DIR="$CHECKOUT/cookies"
JAR="$DIR/cookies.txt"
NEXT="$JAR.new"
PREV="$JAR.prev"

export_is_old=0
if [ "$MODE" = install ]; then
    [ "$EXPORT" != "$(realpath -m -- "$JAR")" ] || fail "$EXPORT is the live jar already; nothing to install"
    check_export "$EXPORT"
    if [ -f "$OLD" ] && [ ! -L "$OLD" ] && [ "$EXPORT" = "$OLD" ]; then
        export_is_old=1
    fi
fi

migrate=0
if [ -L "$OLD" ]; then
    log "$OLD is a symlink to $(readlink "$OLD"), not a jar; nothing to move. go-api never reads that path, so stop dropping jars there"
elif [ -f "$OLD" ] && [ $export_is_old = 0 ]; then
    [ ! -e "$JAR" ] ||
        fail "both $OLD and $JAR exist; if $OLD is the newer export, install it with: bash deploy/cookie-jar-dir.sh $OLD; otherwise delete $OLD"
    migrate=1
fi

if [ "$MODE" = setup ] && [ $migrate = 0 ] && [ ! -f "$JAR" ]; then
    fail "no cookie jar at $JAR; install the browser export with: bash deploy/cookie-jar-dir.sh <export>"
fi
if [ "$MODE" = rollback ] && [ ! -e "$PREV" ]; then
    fail "no previous jar at $PREV to roll back to"
fi

image=""
for candidate in "${GOAPI_IMAGES[@]}"; do
    if docker image inspect "$candidate" >/dev/null 2>&1; then
        image=$candidate
        break
    fi
done
[ -n "$image" ] ||
    fail "none of ${GOAPI_IMAGES[*]} is on this host, so the folder cannot be handed to go-api's group; deploy go-api once and rerun"

gid=$(in_image --entrypoint id "$image" -g) || gid=""
[[ "$gid" =~ ^[0-9]+$ ]] || fail "could not read go-api's gid from $image"

if [ "$MODE" != setup ]; then
    in_image --entrypoint sh "$image" -c 'command -v flock && command -v timeout' >/dev/null ||
        fail "$image has no flock or timeout, so the jar cannot be swapped under go-api's jar lock"
fi

if [ $migrate = 1 ]; then
    mkdir -p "$DIR"
    mv "$OLD" "$JAR"
    log "moved $OLD to $JAR; go-api never reads the old path, so stop dropping jars there"
fi

owner=$(id -u)
mkdir -p "$DIR"
log "giving $DIR to uid $owner and go-api's gid $gid at 2770, and its jar 0660, as root in $image"
in_image -u 0 --entrypoint sh -v "$DIR:/c" "$image" \
    -c "chown $owner:$gid /c && chmod 2770 /c && if [ -e /c/cookies.txt ]; then chown $owner:$gid /c/cookies.txt && chmod 0660 /c/cookies.txt; fi"

swap_in() {
    in_image --entrypoint timeout -v "$DIR:/c" "$image" "$LOCK_WAIT_S" flock /c/cookies.txt.lock sh -c \
        "rm -f /c/cookies.txt.old && if [ -e /c/cookies.txt ]; then cp /c/cookies.txt /c/cookies.txt.old && chmod 0660 /c/cookies.txt.old; fi && mv -f /c/$1 /c/cookies.txt && if [ -e /c/cookies.txt.old ]; then mv -f /c/cookies.txt.old /c/cookies.txt.prev; fi"
}

if [ "$MODE" = install ]; then
    rm -f "$NEXT"
    cp -- "$EXPORT" "$NEXT"
    chmod 0660 "$NEXT"
    if ! swap_in cookies.txt.new; then
        rm -f "$NEXT"
        fail "could not swap in the export under go-api's jar lock within ${LOCK_WAIT_S}s; the live jar is unchanged, so rerun"
    fi
    log "installed the export as $JAR at 0660 in go-api's group, under go-api's jar lock; the jar it replaced is $PREV"
    if [ $export_is_old = 1 ]; then
        rm -f -- "$OLD"
        log "removed $OLD: go-api never reads that path, so stop dropping jars there"
    fi
fi

if [ "$MODE" = rollback ]; then
    swap_in cookies.txt.prev ||
        fail "could not swap back $PREV under go-api's jar lock within ${LOCK_WAIT_S}s; the live jar is unchanged, so rerun"
    log "rolled back: the jar that was $PREV is live again, and the jar it replaced is now $PREV"
fi

log "checking that go-api's user in $image can read the jar and replace files in $DIR"
in_image --entrypoint sh -v "$DIR:/c" "$image" -c 'test -r /c/cookies.txt && test -w /c && test -x /c' ||
    fail "go-api's user in $image cannot read $JAR or replace files in $DIR; $DIR is uid $(stat -c %u "$DIR") gid $(stat -c %g "$DIR") mode $(stat -c %a "$DIR"), want gid $gid mode 2770"

log "done: to drop in a new jar, run bash deploy/cookie-jar-dir.sh <export>, then delete the export; to undo the last one, run bash deploy/cookie-jar-dir.sh --rollback"
