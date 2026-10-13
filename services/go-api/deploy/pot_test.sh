#!/usr/bin/env bash

set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
FAILURES=0

fail() {
    printf 'FAIL: %s\n      %s\n' "$CASE" "$1"
    FAILURES=$((FAILURES + 1))
}

service_block() {
    sed -n "/^  $2:\$/,/^  [a-z][a-z-]*:\$/p" "$1" | sed '$d'
}

expect_pot_service() {
    local file=$1 container=$2 block
    block=$(service_block "$file" pot)
    [ -n "$block" ] || { fail "no pot service in $(basename "$file")"; return; }
    grep -qF "image: brainicism/bgutil-ytdlp-pot-provider:2.0.2" <<<"$block" || fail "pot image is not pinned to 2.0.2"
    grep -qF "container_name: $container" <<<"$block" || fail "pot container is not named $container"
    grep -qF "restart: unless-stopped" <<<"$block" || fail "pot does not restart"
    grep -qE "mem_limit: [0-9]+m" <<<"$block" || fail "pot has no memory limit"
    grep -qE "^\s+ports:" <<<"$block" && fail "pot publishes a host port"
    grep -qF -- "- YTDLP_POT_URL=http://$container:4416" "$file" || fail "go-api is not pointed at http://$container:4416"
}

CASE="prod runs the token provider beside go-api"
expect_pot_service "$HERE/compose.prod.yml" altune-pot

CASE="staging runs its own token provider"
expect_pot_service "$HERE/compose.staging.yml" altune-staging-pot

CASE="staging go-api never reaches the prod token provider"
grep -qF "altune-pot:" "$HERE/compose.staging.yml" && fail "staging points at the prod provider"

CASE="the Dockerfile pins yt-dlp with its challenge solver and the token plugin"
grep -qF 'yt-dlp[default]==2026.8.19' "$HERE/Dockerfile" || fail "yt-dlp[default]==2026.8.19 not pinned"
grep -qF 'bgutil-ytdlp-pot-provider==2.0.2' "$HERE/Dockerfile" || fail "bgutil plugin not pinned"

if [ "$FAILURES" -gt 0 ]; then
    printf '%d failure(s)\n' "$FAILURES"
    exit 1
fi
echo "pot: all cases pass"
