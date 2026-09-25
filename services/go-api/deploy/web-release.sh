#!/usr/bin/env bash

set -euo pipefail

USAGE='usage: web-release.sh <staging|prod> <sha> [tarball]'
WEB_ROOT="${WEB_ROOT:-/home/ubuntu/altune-web}"
RELEASES_KEPT="${RELEASES_KEPT:-5}"
LOCK_TIMEOUT="${LOCK_TIMEOUT:-120}"

log() {
    printf '[web-release] %s\n' "$*" >&2
}

die() {
    log "$*"
    exit 1
}

parse_tier() {
    case "$1" in
        staging | prod) printf '%s' "$1" ;;
        *) die "unknown tier '$1'; $USAGE" ;;
    esac
}

parse_sha() {
    [[ "$1" =~ ^[0-9a-f]{7,40}$ ]] || die "sha '$1' is not a 7-40 char lowercase hex commit; $USAGE"
    printf '%s' "$1"
}

take_release_lock() {
    exec 9>"$TIER_DIR/.release.lock"
    flock -w "$LOCK_TIMEOUT" 9 || die "another web release for $TIER held the lock for ${LOCK_TIMEOUT}s"
}

unpack_release() {
    local tarball=$1 staging_dir
    [ -f "$tarball" ] || die "no release $SHA on disk and no tarball at '$tarball'"
    staging_dir=$(mktemp -d "$TIER_DIR/releases/.unpack-$SHA.XXXXXX")
    if ! tar -xzf "$tarball" -C "$staging_dir" --no-same-owner; then
        rm -rf "$staging_dir"
        die "could not unpack $tarball; current is untouched"
    fi
    if [ ! -f "$staging_dir/index.html" ]; then
        rm -rf "$staging_dir"
        die "$tarball has no index.html at its root; current is untouched"
    fi
    chmod 755 "$staging_dir"
    mv -T "$staging_dir" "$TIER_DIR/releases/$SHA"
    log "unpacked $SHA"
}

flip_current() {
    local next_link="$TIER_DIR/.current-$SHA.$$"
    ln -sfn "releases/$SHA" "$next_link"
    mv -T "$next_link" "$TIER_DIR/current"
    touch "$TIER_DIR/releases/$SHA"
    log "$TIER now serves $SHA"
}

prune_old_releases() {
    local stale
    find "$TIER_DIR/releases" -mindepth 1 -maxdepth 1 -name '.unpack-*' -exec rm -rf {} +
    find "$TIER_DIR/releases" -mindepth 1 -maxdepth 1 -type d -printf '%T@ %f\n' |
        sort -rn | tail -n +"$((RELEASES_KEPT + 1))" | while read -r _ stale; do
        rm -rf "${TIER_DIR:?}/releases/$stale"
        log "pruned $stale"
    done
}

case $# in 2 | 3) ;; *) die "$USAGE" ;; esac
TIER=$(parse_tier "$1")
SHA=$(parse_sha "$2")
TIER_DIR="$WEB_ROOT/$TIER"

mkdir -p "$TIER_DIR/releases"
take_release_lock
if [ ! -d "$TIER_DIR/releases/$SHA" ]; then
    unpack_release "${3:-}"
fi
flip_current
prune_old_releases
