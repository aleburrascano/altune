#!/usr/bin/env bash

set -euo pipefail

cd "$(dirname "$0")/.." || exit 1

CHECKOUT=$(cd ../.. && pwd)
OLD="$CHECKOUT/cookies.txt"
DIR="$CHECKOUT/cookies"
JAR="$DIR/cookies.txt"
GOAPI_IMAGES=(altune-go-api:blue altune-go-api:green altune-staging-go-api:blue altune-staging-go-api:green)

log() {
    printf '[cookie-jar-dir] %s\n' "$*" >&2
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

if [ ! -f "$JAR" ]; then
    log "FAILED: no cookie jar at $JAR; export one from the browser to that path and rerun"
    exit 1
fi

chmod 2770 "$DIR"
chmod 0660 "$JAR"
log "set $DIR to 2770 and its jar to 0660"

image=""
for candidate in "${GOAPI_IMAGES[@]}"; do
    if docker image inspect "$candidate" >/dev/null 2>&1; then
        image=$candidate
        break
    fi
done
if [ -z "$image" ]; then
    log "FAILED: none of ${GOAPI_IMAGES[*]} is on this host, so go-api's access cannot be checked; deploy go-api once and rerun"
    exit 1
fi

log "checking that go-api's user in $image can read the jar and replace files in $DIR"
if ! docker run --rm --pull never --network none --entrypoint sh -v "$DIR:/jar" "$image" \
    -c 'test -r /jar/cookies.txt && test -w /jar && test -x /jar'; then
    log "FAILED: go-api's user in $image cannot read $JAR or replace files in $DIR; $DIR is owned by uid $(stat -c %u "$DIR") gid $(stat -c %g "$DIR"), which must match go-api's uid or gid"
    exit 1
fi

log "done: to drop in a new jar, copy it to $JAR.new and then mv it over $JAR; go-api picks it up on its next yt-dlp run"
