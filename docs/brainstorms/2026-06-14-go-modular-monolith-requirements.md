---
date: 2026-06-14
topic: go-modular-monolith
---

# Go Modular Monolith Migration

## Summary

Rewrite Altune's Python (FastAPI) backend as a Go modular monolith — one binary with four internal modules (catalog, acquisition, discovery, auth) plus shared infrastructure. Port modules sequentially, deploy when the full Go monolith is ready. Containerize with Docker Compose on the existing OCI instance.

---

## Problem Frame

Altune's backend is a Python 3.12 FastAPI hexagonal monolith organized layer-first (`domain/`, `application/`, `adapters/`). It works, but the developer has identified concrete friction:

- **Language fit.** Python's interpreted nature and dynamic typing create a ceiling on performance and type safety that Go eliminates with compiled binaries and a strong static type system.
- **Code organization.** The layer-first hexagonal layout scatters each bounded context across 3+ directories. Understanding "catalog" requires reading files from `domain/catalog/`, `application/catalog/`, `adapters/inbound/http/catalog/`, and `adapters/outbound/persistence/catalog/`. This increases context load for AI-assisted development, which is the primary development mode.
- **Learning goals.** Containerization (Docker, deployment orchestration) and Go idioms are explicit learning objectives — valuable for school and professional development.
- **Future growth.** The project is pre-launch but intended as a production system. Reorganizing now, before users exist, avoids a costlier migration later.

The current backend has ~82 Python files across 2 bounded contexts (catalog with nested acquisition, discovery) plus cross-cutting auth and platform infrastructure.

---

## Requirements

**Architecture**

- R1. The Go backend is a single binary (modular monolith), not a set of microservices. All modules compile into one executable.
- R2. Code is organized context-first: each module owns its full stack (domain, application, ports, handlers) under `internal/<module>/`.
- R3. Four modules: `catalog` (tracks, playlists, streaming, library), `acquisition` (audio download pipeline), `discovery` (search, providers, ranking, history, clicks), `auth` (JWT verification middleware).
- R4. Shared infrastructure (config, database, logging) lives in `internal/shared/` or a similar cross-cutting package.
- R5. Module dependencies flow in one direction: acquisition may import catalog domain types; no circular imports.
- R6. The Go monolith exposes the same `/v1/*` HTTP endpoint paths as the current Python API — the mobile client must not need changes.

**Data and infrastructure**

- R7. The Go monolith connects to the existing Supabase-hosted Postgres. No database migration, no schema-per-module separation.
- R8. Redis remains for discovery caches (query cache, artwork cache, MBID cache, popularity cache, content validation cache, fetch success store) — Go Redis client replaces Python Redis client.
- R9. OCI Object Storage access (S3-compatible, boto3 today) is re-implemented with a Go S3 client for audio storage and streaming.
- R10. JWT verification (currently Supabase JWKS-based RS256) is re-implemented in Go with equivalent behavior.

**Migration approach**

- R11. Modules are ported one at a time in order: shared infrastructure → catalog → acquisition → discovery. Each module is tested before the next begins.
- R12. The Python monolith continues running for the developer's use during the rewrite. The Go monolith replaces it only when all modules are ported and verified.
- R13. No reverse proxy or dual-routing is needed during migration — the switch is a single cutover when Go is ready.

**Deployment**

- R14. Each environment (Go API, Postgres, Redis) runs in a Docker container orchestrated by Docker Compose.
- R15. The Go binary is deployed to the existing OCI instance (`151.145.41.81`) via Docker Compose.
- R16. A Dockerfile for the Go API produces a minimal production image (multi-stage build).

**Domain fidelity**

- R17. Go domain types match the Python domain model exactly: same aggregates (Track, Playlist), same value objects, same invariants, same events. The ubiquitous language glossary (`docs/ubiquitous-language.md`) is the source of truth.
- R18. The acquisition pipeline preserves the same 6-step structure (search → select → download → tag → store → update_track) with rollback on failure.
- R19. The discovery engine preserves dedup/ranking behavior: identifier-only merge (MBID/ISRC), RRF scoring, quality scoring, circuit breakers.

---

## Acceptance Examples

- AE1. **Covers R6, R12.** Given the Python API is running and serving the mobile app, when the Go monolith is deployed and the mobile app's `EXPO_PUBLIC_API_URL` is pointed at it, all existing features (library browsing, search, playlists, streaming, acquisition) work without mobile code changes.
- AE2. **Covers R5.** Given `internal/acquisition/` imports `internal/catalog/domain/`, when `internal/catalog/` is inspected, it has no imports from `internal/acquisition/` or `internal/discovery/`.
- AE3. **Covers R17.** Given a Track with `acquisition_status = READY` and `audio_ref` set in Postgres, when the Go catalog handler serves `GET /v1/tracks`, the response JSON matches the Python API's `TrackResponse` schema field-for-field.

---

## Success Criteria

- The mobile app works identically against the Go backend as it did against Python — no regressions in library, search, playlists, streaming, or acquisition.
- Each Go module has its own test suite achieving at least the coverage level of the corresponding Python tests.
- The Go binary deploys on the OCI instance via `docker compose up` with no manual setup beyond initial Docker installation.
- The developer can hand any single module directory (`internal/catalog/`, etc.) to Claude for reasoning without needing files from other modules.

---

## Scope Boundaries

- Microservices extraction (separate binaries, inter-service communication, message broker) — deferred indefinitely
- API gateway / service mesh / reverse proxy — not needed for single binary
- Database-per-module or schema-per-module isolation — not needed
- Kubernetes or container orchestration beyond Docker Compose — not needed for single-node deployment
- CI/CD pipeline — can be added later, not blocking the rewrite
- Event schema design / async messaging — in-process function calls, same as current Python
- New features — the Go rewrite is a 1:1 port of existing behavior, not a feature expansion
- Mobile client changes — the Go API preserves the current HTTP contract

---

## Key Decisions

- **Go modular monolith over microservices**: delivers Go's performance, type safety, and containerization learning without the operational tax of multi-service deployment, message brokers, and distributed debugging. Microservices can be extracted later along module boundaries if needed.
- **Acquisition as a separate module from catalog**: the download pipeline (12 files, CPU-bound, 6-step sequential with rollback) is fundamentally different from catalog's CRUD operations. Separating them makes each module easier to reason about.
- **Context-first over layer-first organization**: `internal/<module>/` groups all code for a bounded context in one directory, reducing the file scatter that makes AI-assisted development harder with the current Python layout.
- **Module-by-module rewrite over strangler fig**: with no production users, the dual-system coordination of strangler fig (reverse proxy, dual routing) adds complexity without the zero-downtime benefit. Sequential porting with a single cutover is simpler for a solo developer.
- **Supersedes ADR-0002**: the original stack decision rejected Go citing "ecosystem for music metadata weaker; dev not idiomatic." The developer's Go familiarity has grown, and the metadata ecosystem concern is mitigated by Go's FFI and subprocess capabilities (yt-dlp, ffmpeg are external processes regardless of language).

---

## Dependencies / Assumptions

- The OCI Object Storage migration is shipped — audio streams from S3-compatible bucket, not SSH.
- Supabase Postgres schema is stable and accessible from Go via a Postgres driver (pgx or equivalent).
- Supabase Auth (JWKS endpoint) is accessible from Go for JWT verification.
- yt-dlp and ffmpeg remain external CLI tools called via subprocess — no Go-native replacement needed.
- Redis is available on the OCI instance or via a managed service for discovery caches.

---

## Outstanding Questions

### Deferred to Planning

- [Affects R9][Technical] Which Go S3 client library to use for OCI Object Storage (aws-sdk-go-v2 or minio-go)?
- [Affects R10][Technical] Which Go JWT library to use for Supabase JWKS verification?
- [Affects R7][Technical] Which Go Postgres driver/ORM approach (pgx raw, sqlc, sqlx, GORM, or Ent)?
- [Affects R8][Technical] Which Go Redis client (go-redis or redigo)?
- [Affects R18][Technical] How to invoke yt-dlp and ffmpeg from Go — os/exec subprocess or a Go binding?
- [Affects R14][Needs research] Docker Compose setup for OCI instance — existing Docker installation status, firewall/port configuration.
- [Affects R16][Technical] Go multi-stage Dockerfile pattern — Alpine vs scratch vs distroless base image.
- [Affects R2][Technical] Exact Go package layout conventions — whether to use `app/` vs `service/` vs `usecase/` for the application layer naming.
