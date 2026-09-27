#!/usr/bin/env bash

set -uo pipefail

node scripts/test-home.mjs "origin/$BASE_REF"
rc=$?
if [ "$rc" -eq 3 ]; then echo "::warning::test-home could not run; not blocking"; exit 0; fi
exit "$rc"
