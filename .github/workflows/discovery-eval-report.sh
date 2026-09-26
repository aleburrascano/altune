#!/usr/bin/env bash

set -uo pipefail

go run ./cmd/discoveryeval -mode report -reports ./tmp/reports > ./tmp/summary.md
code=$?
cat ./tmp/summary.md >> "$GITHUB_STEP_SUMMARY"
cat ./tmp/summary.md
exit $code
