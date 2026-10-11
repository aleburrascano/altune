#!/usr/bin/env bash

set -euo pipefail

USAGE='usage: ota-release.sh <tarball>'
UPDATES_ROOT="${UPDATES_ROOT:-$HOME/altune-web/updates/prod}"
LOCK_TIMEOUT="${LOCK_TIMEOUT:-120}"

log() {
    printf '[ota-release] %s\n' "$*" >&2
}

die() {
    log "$*"
    exit 1
}

the_one_manifest() {
    local found
    found=$(cd "$1" && find manifests -type f 2>/dev/null)
    if [ -z "$found" ] || [ "$(printf '%s\n' "$found" | wc -l)" != 1 ]; then
        die "the tarball must hold exactly one manifests/<runtime>/<platform>, found: ${found:-none}"
    fi
    printf '%s' "$found"
}

[ $# = 1 ] || die "$USAGE"
[ -f "$1" ] || die "no tarball at '$1'"
mkdir -p "$UPDATES_ROOT/assets" "$UPDATES_ROOT/manifests"
exec 9>"$UPDATES_ROOT/.release.lock"
flock -w "$LOCK_TIMEOUT" 9 || die "another update release held the lock for ${LOCK_TIMEOUT}s"

unpacked=$(mktemp -d "$UPDATES_ROOT/.unpack.XXXXXX")
trap 'rm -rf "$unpacked"' EXIT
tar -xzf "$1" -C "$unpacked" --no-same-owner || die "could not unpack $1; nothing changed"
manifest=$(the_one_manifest "$unpacked")

cp -rn "$unpacked/assets/." "$UPDATES_ROOT/assets/"
mkdir -p "$UPDATES_ROOT/$(dirname "$manifest")"
cp "$unpacked/$manifest" "$UPDATES_ROOT/$manifest.next"
mv -T "$UPDATES_ROOT/$manifest.next" "$UPDATES_ROOT/$manifest"
log "now serving $manifest"
