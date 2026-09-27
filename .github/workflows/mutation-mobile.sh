#!/usr/bin/env bash

set -euo pipefail

if [ -n "${MUTATE:-}" ]; then
  npx stryker run --mutate "$MUTATE"
else
  npm run mutate
fi
