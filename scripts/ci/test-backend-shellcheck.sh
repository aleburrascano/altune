#!/usr/bin/env bash

set -euo pipefail

cd "$(dirname "$0")/../../services/go-api" >/dev/null 2>&1 || { echo "test-backend-shellcheck: services/go-api was not found beside $(dirname "$0")/../.., so nothing was checked" >&2; exit 2; }

if command -v shellcheck >/dev/null 2>&1; then
  shellcheck -x --exclude=SC2148 deploy/*.sh deploy/duckdns
else
  echo "shellcheck not on runner; skipping deploy-script lint"
fi
