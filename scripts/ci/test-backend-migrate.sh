#!/usr/bin/env bash

set -euo pipefail

cd "$(dirname "$0")/../../services/go-api" >/dev/null 2>&1 || { echo "test-backend-migrate: services/go-api was not found beside $(dirname "$0")/../.., so no migration was applied" >&2; exit 2; }

for f in migrations/*.sql; do
  echo "applying $f"
  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -q -f "$f"
done
