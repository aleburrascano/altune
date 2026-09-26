#!/usr/bin/env bash

set -euo pipefail

USAGE='usage: release.sh staging <sha> | release.sh prod <sha> <public-url>'
ALTUNE_DIR="${ALTUNE_DIR:-$HOME/altune}"
LOCK_FILE="${LOCK_FILE:-$HOME/.altune-staging.lock}"
LOCK_TIMEOUT="${LOCK_TIMEOUT:-600}"

die() {
    printf '[release] %s\n' "$*" >&2
    exit 1
}

main() {
    local tier=${1:-} sha=${2:-} url=${3:-}
    case "$tier" in
        staging) ;;
        prod) [ -n "$url" ] || die "prod needs a public url; $USAGE" ;;
        *) die "unknown tier '$tier'; $USAGE" ;;
    esac
    [[ "$sha" =~ ^[0-9a-f]{40}$ ]] || die "sha '$sha' is not a 40 char lowercase hex commit; $USAGE"

    exec 9>"$LOCK_FILE"
    flock -w "$LOCK_TIMEOUT" 9 || die "timed out waiting for $LOCK_FILE"
    git -C "$ALTUNE_DIR" fetch --prune origin
    git -C "$ALTUNE_DIR" checkout main
    git -C "$ALTUNE_DIR" reset --hard "$sha"
    cd "$ALTUNE_DIR/services/go-api"

    if [ "$tier" = staging ]; then
        bash deploy/staging.sh
    else
        bash deploy/prod-migrate.sh
        bash deploy/blue-green.sh
        bash deploy/overseer.sh
        SMOKE_GOAPI_CONTAINER="altune-go-api-$(. deploy/lib.sh && active_color)" \
            bash deploy/smoke.sh "$url" altune-overseer "$sha"
    fi
    docker image prune -f
}

main "$@"
