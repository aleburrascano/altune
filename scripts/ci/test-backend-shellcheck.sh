#!/usr/bin/env bash

set -euo pipefail

if command -v shellcheck >/dev/null 2>&1; then
  shellcheck -x --exclude=SC2148 deploy/*.sh deploy/duckdns
else
  echo "shellcheck not on runner; skipping deploy-script lint"
fi
