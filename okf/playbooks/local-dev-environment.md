---
type: Playbook
title: Local dev environment
description: How to stand up Postgres/Redis and hot-reload the Go API for local development.
resource: docker-compose.yml, services/go-api/docker-compose.yml, services/go-api/.env.example, services/go-api/.air.toml
tags: [local-dev, docker-compose, air, hot-reload, environment-variables]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

Two nearly-identical compose files exist at different scopes, and their difference is the gotcha.

**Root `docker-compose.yml`** (repo root) is the canonical local-dev infra file: `postgres:16-alpine` (container `altune-postgres-dev`, user/db `altune`, password `altune_dev`, port 5432, named volume `altune-postgres-data`) and `redis:7-alpine` (container `altune-redis-dev`, port 6379, named volume `altune-redis-data`). Both carry healthchecks (`pg_isready` / `redis-cli ping`, 5s interval, 10 retries). Explicit header comment: production runs on a separate Supabase project (ADR-0003) and this file is dev-only; `testcontainers-python` integration tests spin their own ephemeral Postgres and don't touch this file at all. Bring up with `docker compose up -d`; wipe with `docker compose down -v`.

**`services/go-api/docker-compose.yml`** is a second, separate file — its own header says it's "used for production deployment on OCI instance," but it's actually the go-api-plus-infra bundle: it adds a `go-api` service (builds from the local `Dockerfile`, port 8000, reads `.env` via `env_file`) alongside its own `postgres`/`redis` definitions (non-`-dev` container names, password templated as `${POSTGRES_PASSWORD}`, unnamed default volumes). This is superseded in practice by `docker-compose.prod.yml` for actual production (see [production-deployment](production-deployment.md)) — don't confuse the three files.

**Hot reload — `.air.toml`**: `air` watches `.go`/`.toml` files (excludes `tmp/`, `vendor/`, `node_modules/`, and `_test.go` files), rebuilds with `go build -o ./tmp/api.exe ./cmd/api`, and restarts `tmp/api.exe` after a 500ms debounce. Per `services/go-api/CLAUDE.md`, plain `go build && ./tmp/api.exe` also works without hot reload; code changes never take effect until rebuild+restart either way.

**Required env vars (`.env.example`, categories only — no secrets)**: environment/logging/host/port; CORS origins; `DATABASE_URL` (local Postgres or Supabase — session-mode pooler port 5432, NOT transaction-mode 6543); Supabase Auth JWKS config (HS256 unsupported) + `SUPABASE_ANON_KEY`; `REDIS_URL` (optional — caches degrade gracefully absent, see [shared-infra](../backend/shared-infra.md)); a block of discovery-provider API keys (MusicBrainz, Last.fm, Fanart.tv, Genius, Discogs, YouTube — all optional, feature disables when unset); OCI S3 object storage config + local `MUSIC_DIR` fallback; audio-acquisition tool paths (ffmpeg, yt-dlp cookies/JS runtime, see [acquisition](../backend/acquisition.md)); Mission Control operator gate (`OPERATOR_USER_ID`, fail-closed — `/admin` denied to everyone until set) + `ALERT_NTFY_URL`; and a block of discovery-telemetry/ranking-experiment flags (`EVAL_METER_ENABLED`, `ALERT_ZERO_RESULT_THRESHOLD`, `BEHAVIORAL_CORPUS_PATH`, `BEHAVIORAL_RANKING_ENABLED`, `EXPLORATION_ENABLED`/`EXPLORATION_RATE`, `TAIL_DEMOTION_ENABLED`, `CROSS_KIND_PROMINENCE_ENABLED`) — most default `false`/empty and are explicitly eval-A/B-gated before flipping on, per inline comments.

**Gotcha**: two `docker-compose.yml` files with overlapping service names but different container names/passwords/volumes exist side-by-side — always check which directory you're in before running `docker compose` commands.
