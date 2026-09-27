#!/usr/bin/env bash

set -euo pipefail

for f in migrations/*.sql; do
  echo "applying $f"
  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -q -f "$f"
done
