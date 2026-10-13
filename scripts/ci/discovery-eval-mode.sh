#!/usr/bin/env bash

set -uo pipefail

for attempt in 1 2; do
  if ./tmp/discoveryeval \
    -mode "$MODE" \
    -corpus-file "$CORPUS" \
    -json "./tmp/report-$MODE.json" \
    -metrics "./tmp/metrics-$MODE.json"; then
    exit 0
  fi
  echo "$MODE regressed or errored (attempt $attempt/2)"
  sleep "$RETRY_DELAY"
done
exit 1
