#!/usr/bin/env bash

set -uo pipefail

go tool cover -func=coverage.out | tail -1
PCT=$(go tool cover -func=coverage.out | tail -1 | grep -oE '[0-9]+\.[0-9]+')
echo "total coverage: ${PCT}% (floor ${MIN}%)"
awk -v p="$PCT" -v m="$MIN" 'BEGIN { exit (p >= m) ? 0 : 1 }' \
  || { echo "::error::coverage ${PCT}% fell below the ${MIN}% floor"; exit 1; }
