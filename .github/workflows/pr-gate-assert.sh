#!/usr/bin/env bash

set -uo pipefail

fail=0
for name in BACKEND MOBILE OVERSEER CYCLES TEST_HOME; do
  result="${!name}"
  case "$result" in
    success|skipped) echo "$name: $result" ;;
    *) echo "$name gate did not pass: $result"; fail=1 ;;
  esac
done
[ "$fail" -eq 0 ] || exit 1
echo "all applicable suites passed"
