#!/usr/bin/env bash


set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
env_file=${UPTIME_ENV_FILE:-$HOME/.config/altune/uptime.env}
if [ -f "$env_file" ]; then
    set -a
    . "$env_file"
    set +a
fi

report=$(mktemp)
trap 'rm -f "$report"' EXIT

state=up
HEALTH_URL=${UPTIME_HEALTH_URL:-} bash "$HERE/health.sh" >>"$report" 2>&1 || state=down
if [ "$state" = up ] && [ -n "${UPTIME_PROBE_EMAIL:-}" ]; then
    bash "$HERE/journey.sh" >>"$report" 2>&1 || state=down
fi

printf '%s %s: %s\n' "$(date -u +%FT%TZ)" "$state" "$(tr '\n' ' ' <"$report")"

[ "$state" = up ]
