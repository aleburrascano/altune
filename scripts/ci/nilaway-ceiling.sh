#!/usr/bin/env bash

set -euo pipefail

go install go.uber.org/nilaway/cmd/nilaway@571480214735
BASE=0
if [ -n "${1:-}" ]; then
  BASE=$(tr -dc '0-9' < "$1")
fi
if ! command -v nilaway > /dev/null; then
  echo "::error::nilaway is not on PATH; add \$(go env GOPATH)/bin to PATH"
  exit 1
fi
REPORT=$(mktemp)
trap 'rm -f "$REPORT"' EXIT
STATUS=0
GOMEMLIMIT="${GOMEMLIMIT:-4GiB}" nilaway $(go list ./... | grep -v /node_modules/) > "$REPORT" 2>&1 || STATUS=$?
COUNT=$(sed 's/\x1b\[[0-9;]*m//g' "$REPORT" | grep -c 'Potential nil panic detected' || true)
if [ "$STATUS" -ne 0 ] && [ "$COUNT" -eq 0 ]; then
  echo "::error::nilaway exited ${STATUS} without reporting findings"
  cat "$REPORT"
  exit 1
fi
echo "nilaway findings: ${COUNT} (ceiling ${BASE})"
if [ "$COUNT" -gt "$BASE" ]; then
  echo "::error::nilaway findings rose from ${BASE} to ${COUNT} — a new nil-dereference risk was introduced"
  exit 1
fi
