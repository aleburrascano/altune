#!/usr/bin/env bash

set -euo pipefail

MAX=${MAX:-$(cat "$(dirname "${BASH_SOURCE[0]}")/../../client/baselines/complexity-ceiling")}

OUT=$(npm run --silent fallow:health 2>&1 | sed 's/\x1b\[[0-9;]*m//g' || true)
echo "$OUT" | tail -3
SUMMARY=$(echo "$OUT" | grep -E 'above threshold|No issues found' | tail -1 || true)
if [ -z "$SUMMARY" ]; then
  echo "::error::could not parse fallow health output; the complexity ratchet verified nothing"
  exit 1
fi
N=$(echo "$SUMMARY" | grep -oE '[0-9]+ above threshold' | grep -oE '[0-9]+' || echo 0)
echo "complexity findings: ${N} (ceiling ${MAX})"
[ "$N" -le "$MAX" ] || { echo "::error::complexity findings rose to ${N}, above the ${MAX} ceiling"; exit 1; }
