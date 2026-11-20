#!/usr/bin/env bash

set -euo pipefail

cd "$(dirname "$0")/.." || exit 1

CHECKOUT=$(cd ../.. && pwd)
OLD="$CHECKOUT/cookies.txt"
DIR="$CHECKOUT/cookies"
JAR="$DIR/cookies.txt"
APP_UID=1000
HELPER_IMAGE="${COOKIE_HELPER_IMAGE:-alpine:3.21}"

log() {
    printf '[cookie-jar-dir] %s\n' "$*" >&2
}

if [ -f "$OLD" ] && [ ! -L "$OLD" ]; then
    if [ -e "$JAR" ]; then
        log "FAILED: both $OLD and $JAR exist; keep the newer export as $JAR, delete the other, and rerun"
        exit 1
    fi
    mkdir -p "$DIR"
    mv "$OLD" "$JAR"
    ln -s cookies/cookies.txt "$OLD"
    log "moved $OLD to $JAR and left a symlink at the old path"
fi

if [ ! -f "$JAR" ]; then
    log "FAILED: no cookie jar at $JAR; export one from the browser to that path and rerun"
    exit 1
fi

log "giving uid $APP_UID the folder, keeping your group, so go-api can replace the jar and you can still drop one in"
docker run --rm -u 0 -v "$DIR:/jar" "$HELPER_IMAGE" \
    sh -c "chown $APP_UID /jar /jar/cookies.txt && chmod 2775 /jar && chmod 0664 /jar/cookies.txt"

log "done: drop new exports at $JAR (or the symlink $OLD); go-api picks them up on its next yt-dlp run"
