#!/usr/bin/env bash

set -u

if [ -z "${HEALTH_URL:-}" ]; then
  echo "::error::UPTIME_HEALTH_URL is not set in the uptime environment; nothing was probed"
  exit 1
fi

code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 20 "${HEALTH_URL}" || echo 000)
echo "readiness probe → HTTP ${code}"
if [ "${code}" = "200" ]; then
  exit 0
fi

echo "::error::backend readiness probe failed (HTTP ${code})"
exit 1
