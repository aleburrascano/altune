#!/usr/bin/env bash

set -euo pipefail

kind=$1
if [ -z "${PR_BASE:-}" ]; then
  echo "No PR base sha; skipping changed-${kind} check."
  exit 0
fi
echo "Diff base: $PR_BASE"
go run "scripts/lint-changed-${kind}.go" "$PR_BASE"
