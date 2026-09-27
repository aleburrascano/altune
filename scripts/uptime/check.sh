#!/usr/bin/env bash

# Off-box total-down detection, run from cron on a machine other than the prod
# VM (the in-process alert monitor dies with the box, so it cannot report the
# box being fully down). Probes /health, then the find-music journey when the
# probe account is configured, and logs one line per run.
#
# Env file (UPTIME_ENV_FILE, default ~/.config/altune/uptime.env), sourced:
#   UPTIME_HEALTH_URL         required, e.g. https://api.example.com/health
#   UPTIME_SUPABASE_URL, UPTIME_SUPABASE_ANON_KEY, UPTIME_PROBE_EMAIL,
#   UPTIME_PROBE_PASSWORD     the journey probe; skipped while UPTIME_PROBE_EMAIL is unset
#
# Exit: 0 up, 1 down. Self-test: bash scripts/uptime/check_test.sh
#
#   */5 * * * * bash /path/to/scripts/uptime/check.sh >> ~/logs/uptime.log 2>&1

set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
env_file=${UPTIME_ENV_FILE:-$HOME/.config/altune/uptime.env}
if [ -f "$env_file" ]; then
    set -a
    # shellcheck disable=SC1090
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
