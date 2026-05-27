# `services/api/scripts/`

Dev convenience scripts. Not part of the package; not deployed.

## `seed_dev_tracks.sql`

15 sample tracks for the hardcoded dev `user_id` (`00000000-0000-0000-0000-000000000001`, per ADR-0004). Idempotent (`ON CONFLICT (id) DO NOTHING`), so re-running is safe.

Usage:

```bash
# 1. Bring up local Postgres (if not already running).
docker compose up -d postgres

# 2. Apply the schema (idempotent via alembic).
cd services/api && \
  DATABASE_URL="postgresql+asyncpg://altune:altune_dev@localhost:5432/altune" \
  uv run alembic upgrade head

# 3. Seed the tracks.
docker exec -i altune-postgres-dev psql -U altune -d altune \
  < services/api/scripts/seed_dev_tracks.sql

# 4. Smoke-test via curl (with API running on :8000).
curl 'http://127.0.0.1:8000/v1/tracks?limit=5'
```

This script is here for one purpose: letting a developer prove end-to-end that the API serves real data before pointing the mobile app at it. It is **not** the data-import path from the legacy `music-manager`; that's the future `migrate-songs-v1` spec.
