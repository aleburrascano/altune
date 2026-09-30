#!/usr/bin/env bash

set -euo pipefail

go install go.uber.org/nilaway/cmd/nilaway@571480214735
BASE=0
if [ -n "${1:-}" ]; then
  BASE=$(tr -dc '0-9' < "$1")
fi
COUNT=$(GOMEMLIMIT="${GOMEMLIMIT:-4GiB}" nilaway ./... 2>&1 | sed 's/\x1b\[[0-9;]*m//g' | grep -c 'Potential nil panic detected' || true)
echo "nilaway findings: ${COUNT} (ceiling ${BASE})"
if [ "$COUNT" -gt "$BASE" ]; then
  echo "::error::nilaway findings rose from ${BASE} to ${COUNT} — a new nil-dereference risk was introduced"
  exit 1
fi
