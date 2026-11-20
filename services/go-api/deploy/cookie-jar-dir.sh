#!/usr/bin/env bash

set -euo pipefail

[ $# -le 1 ] || { printf '[cookie-jar-dir] usage: bash deploy/cookie-jar-dir.sh [<new cookies export>]\n' >&2; exit 2; }
EXPORT=""
if [ $# -eq 1 ]; then
    [ -f "$1" ] || { printf '[cookie-jar-dir] FAILED: no export at %s\n' "$1" >&2; exit 1; }
    EXPORT=$(realpath -- "$1")
fi

cd "$(dirname "$0")/.." || exit 1

CHECKOUT=$(cd ../.. && pwd)
OLD="$CHECKOUT/cookies.txt"
DIR="$CHECKOUT/cookies"
JAR="$DIR/cookies.txt"
NEXT="$JAR.new"
GOAPI_IMAGES=(altune-go-api:blue altune-go-api:green altune-staging-go-api:blue altune-staging-go-api:green)

log() {
    printf '[cookie-jar-dir] %s\n' "$*" >&2
}

in_image() {
    docker run --rm --pull never --network none "$@"
}

if [ -L "$OLD" ]; then
    log "$OLD is a symlink to $(readlink "$OLD"), not a jar; nothing to move. go-api never reads that path, so stop dropping jars there"
elif [ -f "$OLD" ]; then
    if [ -e "$JAR" ]; then
        log "FAILED: both $OLD and $JAR exist; keep the newer export as $JAR, delete the other, and rerun"
        exit 1
    fi
    mkdir -p "$DIR"
    mv "$OLD" "$JAR"
    log "moved $OLD to $JAR; go-api never reads the old path, so stop dropping jars there"
fi

if [ ! -f "$JAR" ] && [ -z "$EXPORT" ]; then
    log "FAILED: no cookie jar at $JAR; rerun with the browser export: bash deploy/cookie-jar-dir.sh <export>"
    exit 1
fi

image=""
for candidate in "${GOAPI_IMAGES[@]}"; do
    if docker image inspect "$candidate" >/dev/null 2>&1; then
        image=$candidate
        break
    fi
done
if [ -z "$image" ]; then
    log "FAILED: none of ${GOAPI_IMAGES[*]} is on this host, so the folder cannot be handed to go-api's group; deploy go-api once and rerun"
    exit 1
fi

gid=$(in_image --entrypoint id "$image" -g) || gid=""
if ! [[ "$gid" =~ ^[0-9]+$ ]]; then
    log "FAILED: could not read go-api's gid from $image"
    exit 1
fi

owner=$(id -u)
mkdir -p "$DIR"
log "giving $DIR to uid $owner and go-api's gid $gid at 2770, and its jar 0660, as root in $image"
in_image -u 0 --entrypoint sh -v "$DIR:/c" "$image" \
    -c "chown $owner:$gid /c && chmod 2770 /c && if [ -e /c/cookies.txt ]; then chown $owner:$gid /c/cookies.txt && chmod 0660 /c/cookies.txt; fi"

if [ -n "$EXPORT" ]; then
    rm -f "$NEXT"
    cp -- "$EXPORT" "$NEXT"
    chmod 0660 "$NEXT"
    mv -f "$NEXT" "$JAR"
    log "installed the export as $JAR at 0660 in go-api's group; go-api picks it up on its next yt-dlp run"
fi

log "checking that go-api's user in $image can read the jar and replace files in $DIR"
if ! in_image --entrypoint sh -v "$DIR:/c" "$image" -c 'test -r /c/cookies.txt && test -w /c && test -x /c'; then
    log "FAILED: go-api's user in $image cannot read $JAR or replace files in $DIR; $DIR is uid $(stat -c %u "$DIR") gid $(stat -c %g "$DIR") mode $(stat -c %a "$DIR"), want gid $gid mode 2770"
    exit 1
fi

log "done: to drop in a new jar, run bash deploy/cookie-jar-dir.sh <export>; never copy one in by hand, since a copy without group read locks go-api out"
