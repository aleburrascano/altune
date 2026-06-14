---
title: "refactor: Migrate Python backend to Go modular monolith"
type: refactor
status: active
date: 2026-06-14
origin: docs/brainstorms/2026-06-14-go-modular-monolith-requirements.md
---

# refactor: Migrate Python backend to Go modular monolith

## Summary

Port Altune's Python FastAPI backend to a single Go binary organized as a modular monolith with three domain modules (catalog, acquisition, discovery) plus cross-cutting auth and shared infrastructure. Uses pgx+sqlc for persistence, chi for HTTP, minio-go for OCI S3, jwx for JWT, go-redis for caching. Five sequential phases: foundation → catalog → acquisition → discovery → integration. Deploy via Docker Compose on the existing OCI instance.

---

## Problem Frame

The Python monolith works but has friction: interpreted language performance ceiling, dynamic typing, and layer-first code organization that scatters each bounded context across 3+ directories — making AI-assisted development harder. The developer wants Go's compiled performance, static type system, and context-first module organization, plus hands-on containerization experience. No production users exist, so a module-by-module rewrite with single cutover is the lowest-risk migration path. (See origin: `docs/brainstorms/2026-06-14-go-modular-monolith-requirements.md`)

---

## Requirements

- R1. Single Go binary (modular monolith), not microservices
- R2. Context-first organization: each module owns its full stack under `internal/<module>/`
- R3. Three domain modules: catalog, acquisition, discovery. Cross-cutting auth and shared infrastructure are foundational, not standalone modules
- R4. Shared infrastructure in `internal/shared/`
- R5. Module dependencies flow one direction (acquisition → catalog domain; no circular imports)
- R6. Same `/v1/*` HTTP endpoint paths — mobile client unchanged
- R7. Same Supabase Postgres, no schema changes
- R8. Redis caches re-implemented with Go client
- R9. OCI Object Storage via Go S3 client
- R10. JWT verification equivalent to current Supabase JWKS-based RS256
- R11. Modules ported sequentially: shared → catalog → acquisition → discovery
- R12. Python monolith stays running during rewrite; single cutover when Go is ready
- R13. No reverse proxy or dual routing during migration
- R14. Docker Compose deployment (Go API + Postgres + Redis)
- R15. Deploy to OCI instance (151.145.41.81)
- R16. Minimal production Docker image (multi-stage build)
- R17. Go domain types match Python domain model exactly
- R18. Acquisition pipeline preserves 6-step structure with rollback
- R19. Discovery dedup/ranking preserves identifier-only merge, RRF scoring, circuit breakers

**Origin acceptance examples:** AE1 (covers R6, R12), AE2 (covers R5), AE3 (covers R17)

---

## Scope Boundaries

- Microservices extraction — deferred indefinitely
- API gateway / service mesh / reverse proxy — not needed
- Database schema changes or new tables — Go reads existing schema
- New features or behavior changes — 1:1 port only
- Python code deletion — stays in repo until Go is verified
- CI/CD pipeline — deferred to follow-up
- OpenAPI codegen / typed client generation — deferred
- Alembic migration tooling — not ported; future schema changes use golang-migrate
- SSH audio store — dropped; OCI Object Storage is the production path, SSH is deprecated
- HS256 JWT mode — not implemented in Python (raises NotImplementedError), not ported to Go; JWKS/RS256 only

### Deferred to Follow-Up Work

- Retire Python monolith after Go is verified end-to-end
- Set up CI/CD pipeline for Go builds and tests
- OpenAPI spec generation from Go handlers
- Performance benchmarking (Go vs Python baseline)

---

## Context & Research

### Relevant Code and Patterns

The Python backend has ~82 files across 2 bounded contexts:

**Catalog** (~32 files): Track + Playlist aggregates, 9 use cases (CRUD), acquisition pipeline (6 steps), 3 audio store adapters (filesystem, SSH, OCI), SQL repositories

**Discovery** (~43 files): SearchResult/SearchQuery domain types, 6 use cases, 8 provider adapters (Deezer, MusicBrainz, iTunes, LastFm, SoundCloud, TheAudioDB, Wikidata, Genius), 6 Redis cache adapters, dedup/ranking engine, circuit breaker, quality scorer

**Auth** (~3 files): TokenVerifier port, SupabaseJwtVerifier adapter, InvalidTokenError

**Platform** (~7 files): Settings (Pydantic), structured logging, DB engine, Redis client, app factory, DI wiring

### Institutional Learnings

- `docs/solutions/design-patterns/2026-06-08-combined-identity-string-matching-over-field-gates.md`: Acquisition candidate matcher must use combined-identity `token_sort_ratio`, not field-by-field JW gates. Need Go fuzzy matching equivalent.
- `docs/solutions/2026-06-10-type-checking-import-runtime-crash.md`: Meta-lesson — always test success branch of best-effort pipeline stages, not just skip/empty paths.

### External References

- Go modular monolith layout: `cmd/api/` + `internal/<module>/` with domain/ports/service/adapters per module
- pgx v5 + sqlc: type-safe SQL code generation, zero runtime reflection
- chi router: wraps `net/http`, zero framework lock-in
- minio-go v7: built for S3-compatible endpoints, path-style addressing for OCI
- lestrrat-go/jwx v2: full JOSE implementation, JWKS auto-refresh
- go-redis v9: official Redis client, context-aware
- Docker: `golang:1.23-alpine` build → `gcr.io/distroless/static-debian12` runtime

---

## Key Technical Decisions

- **pgx v5 + sqlc over GORM/Ent**: sqlc generates Go structs from raw SQL at compile time. No runtime reflection, no ORM leaking into domain. Port interfaces defined in each module's `ports/` package; sqlc-generated code stays in `adapters/`. golang-migrate for schema migrations.
- **chi over gin/echo/stdlib**: chi handlers are plain `http.HandlerFunc`, middleware is `func(http.Handler) http.Handler`. No framework-specific context types contaminating the adapter layer — exactly what hexagonal architecture wants.
- **minio-go over aws-sdk-go-v2**: minio-go was built for S3-compatible endpoints. OCI Object Storage requires path-style addressing which minio-go handles natively. aws-sdk-go-v2's endpoint resolver hacks for non-AWS services are fragile.
- **lestrrat-go/jwx over golang-jwt**: jwx handles JWKS fetching, kid matching, key rotation, and RS256 verification in one library. golang-jwt requires manual JWKS management.
- **Distroless over Alpine/scratch**: includes CA certificates and timezone data (needed for HTTPS calls to providers and time parsing) without a shell. CGO_ENABLED=0 produces a static binary.
- **Token matching via custom `token_sort_ratio`**: acquisition candidate matching and discovery relevance scoring both depend on rapidfuzz's `token_sort_ratio`. The algorithm: tokenize by whitespace → sort tokens alphabetically → join with space → compute normalized Levenshtein ratio `(2*M)/T * 100`. No Go library implements this exactly; implement it directly (~20 lines) using a Levenshtein distance library. Validate against a test corpus of 50+ input pairs from Python output before use.
- **SoundCloud adapter via yt-dlp subprocess**: Python's SoundCloud adapter uses `yt_dlp.YoutubeDL` as a Python library (not subprocess). Go must use `exec.CommandContext` with yt-dlp CLI instead. Five operations need subprocess mapping: `scsearch` (search), URL lookup, artist top-tracks, artist albums, and album tracks. Use `--dump-json`, `--flat-playlist`, `--no-download` flags. Parse JSON output to match Python's dict structure. This is a material design effort — implementer must validate field mapping parity with real SoundCloud queries.
- **Background acquisition goroutine lifecycle**: acquisitions scheduled via goroutine from `POST /v1/tracks` use a `sync.WaitGroup` tracked in the composition root for graceful shutdown drain, a `context.Context` derived from the server's base context (cancelled on SIGTERM), and a semaphore for concurrency bounds. Each pipeline step checks `ctx.Err()` for cancellation.
- **All port interface methods take `context.Context` and return `error`**: idiomatic Go convention applied uniformly across all port interfaces. No sync methods without context — even `Exists` is a network call in the OCI adapter.
- **CLI subcommands via cobra**: dedup migration, health check, and fix-audio-refs become subcommands of the Go binary (e.g., `go-api migrate-dedup`, `go-api health-check`).

---

## Open Questions

### Resolved During Planning

- **Go Postgres driver**: pgx v5 + sqlc (research confirmed best fit for hexagonal architecture)
- **Go Redis client**: go-redis v9 (official, context-aware, pipeline support)
- **Go S3 client**: minio-go v7 (built for non-AWS S3 endpoints)
- **Go JWT library**: lestrrat-go/jwx v2 (full JWKS auto-refresh)
- **Go HTTP router**: chi (wraps net/http, no lock-in)
- **Docker base image**: distroless/static (CGO_ENABLED=0, includes CA certs)
- **Subprocess management**: exec.CommandContext with context.WithTimeout
- **Go package naming**: `domain/`, `ports/`, `service/`, `adapters/` per module

### Deferred to Implementation

- Exact sqlc query files — depends on reading existing SQLAlchemy models and writing equivalent raw SQL
- Go fuzzy matching library choice — depends on testing `token_sort_ratio` equivalence
- Redis key format — likely preserve existing Python key patterns for backward compatibility during migration
- ffmpeg/yt-dlp binary paths on OCI instance — verify availability during deployment unit

---

## Output Structure

```
services/go-api/
├── cmd/
│   └── api/
│       └── main.go
├── internal/
│   ├── app/
│   │   └── app.go                    # composition root, wiring, server lifecycle
│   ├── shared/
│   │   ├── config/
│   │   │   └── config.go             # env-based settings (mirrors platform/config.py)
│   │   ├── database/
│   │   │   └── database.go           # pgx pool, health check
│   │   ├── redis/
│   │   │   └── redis.go              # go-redis client, graceful degrade
│   │   ├── logging/
│   │   │   └── logging.go            # structured logging (slog)
│   │   ├── httputil/
│   │   │   └── errors.go             # error-to-HTTP mapping
│   │   └── userid.go                 # typed UserId wrapper
│   ├── auth/
│   │   ├── middleware.go              # chi middleware: extract Bearer → verify → inject UserId
│   │   ├── verifier.go               # TokenVerifier port interface
│   │   └── adapters/
│   │       └── supabase_jwt.go       # jwx-based JWKS verification
│   ├── catalog/
│   │   ├── domain/
│   │   │   ├── track.go              # Track aggregate, TrackId, AcquisitionStatus
│   │   │   ├── playlist.go           # Playlist aggregate, PlaylistId, PlaylistTrack
│   │   │   ├── dedup.go              # dedup_key normalizer
│   │   │   └── events.go             # catalog domain events
│   │   ├── ports/
│   │   │   ├── track_repo.go         # TrackRepository interface
│   │   │   ├── playlist_repo.go      # PlaylistRepository interface
│   │   │   └── audio_store.go        # AudioStore interface
│   │   ├── service/
│   │   │   ├── add_track.go
│   │   │   ├── list_tracks.go
│   │   │   ├── delete_track.go
│   │   │   ├── reconcile_status.go
│   │   │   ├── create_playlist.go
│   │   │   ├── list_playlists.go
│   │   │   ├── get_playlist.go
│   │   │   ├── delete_playlist.go
│   │   │   ├── rename_playlist.go
│   │   │   ├── reorder_playlist.go
│   │   │   ├── add_track_to_playlist.go
│   │   │   └── remove_track_from_playlist.go
│   │   └── adapters/
│   │       ├── handler/
│   │       │   ├── track_handler.go   # /v1/tracks routes + DTOs
│   │       │   └── playlist_handler.go # /v1/playlists routes + DTOs
│   │       ├── persistence/
│   │       │   ├── queries/           # sqlc .sql files
│   │       │   ├── sqlc.yaml
│   │       │   ├── track_repo.go      # generated + SqlAlchemy equivalent
│   │       │   └── playlist_repo.go
│   │       └── storage/
│   │           ├── object_storage.go  # minio-go OCI S3 adapter
│   │           └── filesystem.go      # local filesystem adapter
│   ├── acquisition/
│   │   ├── ports/
│   │   │   └── audio_searcher.go      # AudioSearcher interface
│   │   ├── service/
│   │   │   ├── pipeline.go            # step runner with rollback
│   │   │   ├── acquire.go             # main orchestrator
│   │   │   ├── matching.go            # 3-tier candidate selection
│   │   │   └── steps/
│   │   │       ├── search.go
│   │   │       ├── select.go
│   │   │       ├── download.go
│   │   │       ├── tag.go
│   │   │       ├── store.go
│   │   │       └── update_track.go
│   │   └── adapters/
│   │       ├── handler/
│   │       │   └── retry_handler.go   # POST /v1/tracks/{id}/retry-acquisition
│   │       └── ytdlp/
│   │           └── searcher.go        # yt-dlp subprocess wrapper
│   └── discovery/
│       ├── domain/
│       │   ├── search_result.go
│       │   ├── search_query.go
│       │   ├── result_kind.go
│       │   ├── confidence.go
│       │   ├── source_ref.go
│       │   ├── provider_status.go
│       │   ├── provider.go
│       │   ├── search_history.go
│       │   ├── search_click.go
│       │   ├── quality_score.go
│       │   ├── entity_resolution_tier.go
│       │   ├── content_validation_status.go
│       │   └── events.go
│       ├── ports/
│       │   ├── search_provider.go
│       │   ├── query_cache.go
│       │   ├── artwork.go
│       │   ├── history_repo.go
│       │   ├── click_repo.go
│       │   ├── content_provider.go
│       │   ├── mbid_resolver.go
│       │   └── fetch_success.go
│       ├── service/
│       │   ├── search_music.go
│       │   ├── normalize.go
│       │   ├── dedup.go
│       │   ├── circuit_breaker.go
│       │   ├── quality_scorer.go
│       │   ├── url_router.go
│       │   ├── record_click.go
│       │   ├── list_history.go
│       │   ├── get_album_tracks.go
│       │   └── get_artist_content.go
│       └── adapters/
│           ├── handler/
│           │   └── discovery_handler.go
│           ├── persistence/
│           │   ├── queries/
│           │   ├── sqlc.yaml
│           │   ├── history_repo.go
│           │   └── click_repo.go
│           ├── providers/
│           │   ├── deezer.go
│           │   ├── musicbrainz.go
│           │   ├── lastfm.go
│           │   ├── soundcloud.go
│           │   ├── itunes.go
│           │   ├── theaudiodb.go
│           │   ├── wikidata.go
│           │   ├── genius.go
│           │   ├── fanarttv.go
│           │   └── artwork_chain.go
│           └── cache/
│               ├── query_cache.go
│               ├── artwork_cache.go
│               ├── mbid_cache.go
│               ├── popularity_cache.go
│               ├── content_validation.go
│               └── fetch_success.go
├── go.mod
├── go.sum
├── Dockerfile
└── .env.example
```

Plus at the repo root:
```
docker-compose.yml                    # Go API + Postgres + Redis
```

---

## Implementation Units

### Phase 1: Foundation

- U1. **Project scaffolding and config**

**Goal:** Initialize the Go module, directory skeleton, Dockerfile, docker-compose.yml, and environment-based configuration.

**Requirements:** R1, R2, R4, R14, R16

**Dependencies:** None

**Files:**
- Create: `services/go-api/go.mod`
- Create: `services/go-api/cmd/api/main.go`
- Create: `services/go-api/internal/shared/config/config.go`
- Create: `services/go-api/internal/shared/userid.go`
- Create: `services/go-api/Dockerfile`
- Create: `docker-compose.yml`
- Create: `services/go-api/.env.example`
- Test: `services/go-api/internal/shared/config/config_test.go`

**Approach:**
- `go.mod` with module path `github.com/aleburrascano/altune/services/go-api` (or simpler `altune/go-api`)
- Config struct mirrors Python's `Settings` class: env vars for database_url, redis_url, supabase JWT settings, OCI S3 credentials, music_dir, provider API keys. Use `github.com/caarlos0/env` for struct tag parsing
- UserId as a typed UUID wrapper (same pattern as Python's `domain/shared/user_id.py`)
- Dockerfile: multi-stage `golang:1.23-alpine` → `gcr.io/distroless/static-debian12`, CGO_ENABLED=0
- docker-compose.yml: three services (go-api, postgres, redis) with environment variable passthrough

**Patterns to follow:**
- Python config: `services/api/src/altune/platform/config.py` — field names, defaults, validators
- Python UserId: `services/api/src/altune/domain/shared/user_id.py` — typed wrapper pattern

**Test scenarios:**
- Happy path: config loads all required fields from env vars
- Happy path: config applies defaults for optional fields (log_level, host, port)
- Error path: config fails with clear message when required field (database_url) is missing
- Error path: config validates JWT secret XOR JWKS URL (not both, not neither)
- Happy path: UserId wraps UUID, equality by value

**Verification:**
- `go build ./cmd/api` compiles with no errors
- `docker compose build` produces an image
- Config test suite passes

---

- U2. **Database, Redis, and logging**

**Goal:** Set up pgx connection pool, go-redis client with graceful degradation, and structured logging.

**Requirements:** R7, R8

**Dependencies:** U1

**Files:**
- Create: `services/go-api/internal/shared/database/database.go`
- Create: `services/go-api/internal/shared/redis/redis.go`
- Create: `services/go-api/internal/shared/logging/logging.go`
- Test: `services/go-api/internal/shared/database/database_test.go`
- Test: `services/go-api/internal/shared/redis/redis_test.go`

**Approach:**
- pgx v5 pool with `pool.Ping()` health check, `pool_max_conns` from config
- go-redis v9 client with `context.Context` propagation. Graceful degradation: if Redis is unavailable, caches return misses (same behavior as Python)
- Structured logging via `log/slog` (stdlib, Go 1.21+). JSON handler for production, text handler for dev. Equivalent to Python's structlog setup

**Patterns to follow:**
- Python DB: `services/api/src/altune/platform/db.py` — async engine, pool_pre_ping, health check
- Python Redis: graceful degradation pattern used across all cache adapters

**Test scenarios:**
- Happy path: pgx pool connects to Postgres and responds to health check
- Error path: database health check returns error when Postgres is unreachable
- Happy path: Redis client connects and performs GET/SET
- Edge case: Redis client degrades gracefully when Redis is unavailable (returns nil, not error)
- Happy path: logger outputs structured JSON in production mode

**Verification:**
- Database connects to Supabase Postgres with existing credentials
- Redis connects or degrades silently
- Structured log output visible in console

---

- U3. **HTTP server and auth middleware**

**Goal:** Set up chi router with CORS, error-to-HTTP mapping, health endpoint, and JWT auth middleware.

**Requirements:** R6, R10

**Dependencies:** U1, U2

**Files:**
- Create: `services/go-api/internal/shared/httputil/errors.go`
- Create: `services/go-api/internal/auth/middleware.go`
- Create: `services/go-api/internal/auth/verifier.go`
- Create: `services/go-api/internal/auth/adapters/supabase_jwt.go`
- Test: `services/go-api/internal/auth/middleware_test.go`
- Test: `services/go-api/internal/auth/adapters/supabase_jwt_test.go`

**Approach:**
- chi router with `chi.NewRouter()`, `cors.Handler()` middleware, `r.Get("/health", ...)` endpoint
- Error mapping: domain exceptions → HTTP status codes (InvalidTokenError → 401, ValidationError → 422, NotFound → 404)
- Auth middleware extracts `Authorization: Bearer <token>`, calls `TokenVerifier.Verify()`, injects `UserId` into request context
- `TokenVerifier` is a Go interface (port); `SupabaseJwtVerifier` implements it using `lestrrat-go/jwx/v2`
- JWKS fetched from `https://<project>.supabase.co/auth/v1/.well-known/jwks.json` with `jwk.Cache` auto-refresh

**Patterns to follow:**
- Python auth: `services/api/src/altune/platform/auth.py` — Bearer extraction, current_user_id dependency
- Python JWT: `services/api/src/altune/adapters/outbound/auth/supabase_jwt_verifier.py` — JWKS fetch, kid matching, claim validation

**Test scenarios:**
- Happy path: valid JWT with correct claims → UserId injected into context
- Error path: missing Authorization header → 401
- Error path: malformed token (not a JWT) → 401
- Error path: expired token → 401
- Error path: valid JWT but wrong audience → 401
- Happy path: health endpoint returns `{"db": "ok"}` when Postgres is reachable
- Error path: health endpoint returns `{"db": "down"}` when Postgres is unreachable
- Happy path: CORS headers present on preflight requests

**Verification:**
- Auth middleware rejects requests without valid Supabase JWT
- Health endpoint responds at `/health`
- Server starts and listens on configured port

---

### Phase 2: Catalog Module

- U4. **Catalog domain and ports**

**Goal:** Port Track and Playlist aggregates, value objects, events, and repository/store interfaces to Go.

**Requirements:** R2, R3, R5, R17

**Dependencies:** U1

**Files:**
- Create: `services/go-api/internal/catalog/domain/track.go`
- Create: `services/go-api/internal/catalog/domain/playlist.go`
- Create: `services/go-api/internal/catalog/domain/dedup.go`
- Create: `services/go-api/internal/catalog/domain/events.go`
- Create: `services/go-api/internal/catalog/ports/track_repo.go`
- Create: `services/go-api/internal/catalog/ports/playlist_repo.go`
- Create: `services/go-api/internal/catalog/ports/audio_store.go`
- Test: `services/go-api/internal/catalog/domain/track_test.go`
- Test: `services/go-api/internal/catalog/domain/playlist_test.go`
- Test: `services/go-api/internal/catalog/domain/dedup_test.go`

**Approach:**
- Track: struct with fields matching Python (id, user_id, title, artist, album, duration, added_at, artwork_url, acquisition_status, year, genre, track_number, album_artist, isrc, audio_ref, failure_reason). Invariants as methods: `MarkReady(audioRef)` requires audio_ref, `MarkFailed(reason)` requires reason and clears audio_ref
- Playlist: struct with ordered `[]PlaylistTrack` entries. Invariants: name non-empty (max 100), positions contiguous 0..N-1, no duplicate track_ids
- AcquisitionStatus: Go `iota` enum (Pending, Ready, Failed)
- Domain events: immutable structs (TrackAddedToLibrary, PlaylistCreated, etc.)
- Port interfaces: Go interfaces in `ports/` package. `TrackRepository`, `PlaylistRepository`, `AudioStore` — method signatures mirror Python Protocol classes

**Patterns to follow:**
- Python Track: `services/api/src/altune/domain/catalog/track.py` — invariant enforcement methods
- Python Playlist: `services/api/src/altune/domain/catalog/playlist.py` — position validation
- Python ports: `services/api/src/altune/application/catalog/ports.py` — interface signatures

**Test scenarios:**
- Happy path: create Track with valid fields
- Happy path: Track.MarkReady sets status and audio_ref
- Error path: Track.MarkReady without audio_ref panics/errors
- Happy path: Track.MarkFailed sets status, reason, clears audio_ref
- Error path: Track.MarkFailed without reason errors
- Happy path: Playlist.AddTrack appends at correct position
- Happy path: Playlist.RemoveTrack compacts positions
- Edge case: Playlist.AddTrack rejects duplicate track_id
- Edge case: Playlist name validation (empty, over 100 chars)
- Happy path: dedup_key normalizes title+artist+album consistently

**Verification:**
- All domain invariants enforced — same behavior as Python aggregates
- Port interfaces compile (no implementations needed yet)

---

- U5. **Catalog persistence (sqlc)**

**Goal:** Implement TrackRepository and PlaylistRepository using pgx + sqlc against the existing Postgres schema.

**Requirements:** R7, R17

**Dependencies:** U2, U4

**Files:**
- Create: `services/go-api/internal/catalog/adapters/persistence/queries/tracks.sql`
- Create: `services/go-api/internal/catalog/adapters/persistence/queries/playlists.sql`
- Create: `services/go-api/internal/catalog/adapters/persistence/sqlc.yaml`
- Create: `services/go-api/internal/catalog/adapters/persistence/track_repo.go`
- Create: `services/go-api/internal/catalog/adapters/persistence/playlist_repo.go`
- Test: `services/go-api/internal/catalog/adapters/persistence/track_repo_test.go`
- Test: `services/go-api/internal/catalog/adapters/persistence/playlist_repo_test.go`

**Approach:**
- Write raw SQL queries matching existing SQLAlchemy behavior. Key queries: `INSERT ... ON CONFLICT (user_id, dedup_key) DO NOTHING` for idempotent track adds, `SELECT ... ORDER BY added_at DESC, id DESC` for paginated listing
- sqlc generates type-safe Go code from .sql files. sqlc.yaml configures the output package
- Repository structs implement the port interfaces from U4, wrapping sqlc-generated functions
- Domain ↔ row mapping functions (equivalent to Python's `to_domain()` / `from_domain()`)

**Patterns to follow:**
- Python track repo: `services/api/src/altune/adapters/outbound/persistence/catalog/track_repository.py` — INSERT ON CONFLICT, pagination, domain mapping
- Python playlist repo: `services/api/src/altune/adapters/outbound/persistence/catalog/playlist_repository.py` — join queries, position management

**Test scenarios:**
- Happy path: add track returns (Track, true) on first insert
- Happy path: add track returns (Track, false) on dedup hit (same user_id + dedup_key)
- Happy path: list tracks returns paginated results ordered by added_at DESC
- Edge case: list tracks with offset beyond total returns empty list with has_more=false
- Happy path: update track persists all changed fields
- Error path: update track with mismatched user_id returns not-found (0 rows affected) — sqlc UPDATE must include `AND user_id = $N`
- Happy path: delete track removes row
- Happy path: create playlist, add track, get playlist with tracks
- Happy path: remove track from playlist compacts positions
- Happy path: reorder playlist tracks updates positions

**Verification:**
- All repository tests pass against a real Postgres instance (testcontainers or Supabase dev)
- Domain objects round-trip through persistence unchanged

---

- U6. **Catalog use cases and HTTP handlers**

**Goal:** Port all 9 catalog use cases and the track/playlist HTTP handlers with DTOs.

**Requirements:** R6, R17

**Dependencies:** U3, U4, U5

**Files:**
- Create: `services/go-api/internal/catalog/service/add_track.go`
- Create: `services/go-api/internal/catalog/service/list_tracks.go`
- Create: `services/go-api/internal/catalog/service/delete_track.go`
- Create: `services/go-api/internal/catalog/service/reconcile_status.go`
- Create: `services/go-api/internal/catalog/service/playlist_*.go` (create, list, get, delete, rename, reorder, add_track, remove_track)
- Create: `services/go-api/internal/catalog/adapters/handler/track_handler.go`
- Create: `services/go-api/internal/catalog/adapters/handler/playlist_handler.go`
- Test: `services/go-api/internal/catalog/service/add_track_test.go`
- Test: `services/go-api/internal/catalog/service/list_tracks_test.go`
- Test: `services/go-api/internal/catalog/adapters/handler/track_handler_test.go`
- Test: `services/go-api/internal/catalog/adapters/handler/playlist_handler_test.go`

**Approach:**
- Each use case is a function or method on a service struct, accepting port interfaces via constructor injection
- AddTrack: dedup check via natural key, emit TrackAddedToLibrary event (log only, same as Python)
- ListTracks: paginated with total count and has_more (same envelope as Python ListTracksResponse)
- HTTP handlers: thin chi route handlers. Parse request → call use case → serialize DTO response
- DTOs: Go structs with JSON tags matching Python's Pydantic response models field-for-field
- Background acquisition: on POST /v1/tracks, schedule acquisition in a goroutine (equivalent to Python's BackgroundTasks)

**Patterns to follow:**
- Python use cases: `services/api/src/altune/application/catalog/` — each file is one use case
- Python handlers: `services/api/src/altune/adapters/inbound/http/catalog/router.py` — route structure, status codes
- Python DTOs: `services/api/src/altune/adapters/inbound/http/catalog/dto.py` — response field names

**Test scenarios:**
- Happy path: POST /v1/tracks with valid body → 201, track created
- Happy path: POST /v1/tracks with existing dedup_key → 200, existing track returned
- Happy path: GET /v1/tracks → paginated response with total, tracks, has_more
- Happy path: DELETE /v1/tracks/{id} → 204
- Error path: DELETE /v1/tracks/{id} for nonexistent track → 404
- Error path: POST /v1/tracks without auth → 401
- Happy path: full playlist CRUD lifecycle (create → add tracks → get with tracks → reorder → remove → delete)
- Integration: POST /v1/tracks triggers background acquisition (goroutine started)

**Verification:**
- HTTP responses match Python API field-for-field (compare JSON schemas)
- Status codes match Python behavior (201 create, 200 dedup, 204 delete)

---

- U7. **OCI Object Storage adapter and audio streaming**

**Goal:** Implement AudioStore using minio-go for OCI S3, and the audio streaming endpoint.

**Requirements:** R9, R6

**Dependencies:** U4, U6

**Files:**
- Create: `services/go-api/internal/catalog/adapters/storage/object_storage.go`
- Create: `services/go-api/internal/catalog/adapters/storage/filesystem.go`
- Test: `services/go-api/internal/catalog/adapters/storage/object_storage_test.go`

**Approach:**
- `ObjectStorageAudioStore` implements `AudioStore` port using minio-go v7 client
- Methods: `Exists(ctx, audioRef) bool`, `Store(ctx, src, audioRef) error`, `Stream(ctx, audioRef) (io.ReadCloser, error)`, `ResolvePath(ctx, audioRef) (string, error)` for local fallback
- Audio streaming endpoint `GET /v1/tracks/{id}/audio`: verify track ownership, check status=READY, call `AudioStore.Stream()`, pipe to `http.ResponseWriter` with `Content-Type: audio/mpeg`
- Reconciliation: if stream fails (file missing), trigger ReconcileTrackStatus (mark track FAILED) — same as Python's Fix-Log-Signal cascade
- FilesystemAudioStore as local dev fallback (same priority chain: OCI > filesystem)

**Patterns to follow:**
- Python OCI adapter: `services/api/src/altune/adapters/outbound/audio/object_storage_store.py` — boto3 client, stream method
- Python streaming: `services/api/src/altune/adapters/inbound/http/catalog/router.py` stream_audio handler

**Test scenarios:**
- Happy path: Stream returns audio bytes from OCI Object Storage
- Happy path: Store uploads file to OCI bucket at correct key
- Happy path: Exists returns true for existing object
- Error path: Stream for missing object triggers reconciliation (track marked FAILED)
- Happy path: GET /v1/tracks/{id}/audio returns audio/mpeg with correct headers
- Error path: GET /v1/tracks/{id}/audio for track with status != READY → appropriate error
- Error path: GET /v1/tracks/{id}/audio for another user's track → 404

**Verification:**
- Audio streams from OCI Object Storage through the Go endpoint
- Mobile app can play audio via the Go streaming endpoint

---

### Phase 3: Acquisition Module

- U8. **Acquisition pipeline and matching logic**

**Goal:** Port the 6-step acquisition pipeline with rollback, candidate matching (3-tier selection), and pipeline context.

**Requirements:** R3, R5, R18

**Dependencies:** U4 (catalog domain types)

**Files:**
- Create: `services/go-api/internal/acquisition/ports/audio_searcher.go`
- Create: `services/go-api/internal/acquisition/service/pipeline.go`
- Create: `services/go-api/internal/acquisition/service/acquire.go`
- Create: `services/go-api/internal/acquisition/service/matching.go`
- Create: `services/go-api/internal/acquisition/service/steps/search.go`
- Create: `services/go-api/internal/acquisition/service/steps/select.go`
- Create: `services/go-api/internal/acquisition/service/steps/download.go`
- Create: `services/go-api/internal/acquisition/service/steps/tag.go`
- Create: `services/go-api/internal/acquisition/service/steps/store.go`
- Create: `services/go-api/internal/acquisition/service/steps/update_track.go`
- Test: `services/go-api/internal/acquisition/service/matching_test.go`
- Test: `services/go-api/internal/acquisition/service/pipeline_test.go`
- Test: `services/go-api/internal/acquisition/service/steps/search_test.go`

**Approach:**
- Pipeline runner: sequential step execution with reverse rollback on failure (same as Python's `run_pipeline`)
- AcquisitionContext: mutable struct carrying track, candidates, selected, temp_path, audio_ref across steps
- Matching: 3-tier candidate selection (Topic channels first, metadata rank second, identity fallback). Use Go fuzzy matching library for `token_sort_ratio` on combined identity strings per institutional learning
- Search step: 4-tier waterfall (ISRC → title+artist → title+artist+album → title+artist+audio), dedup URLs
- Download step: call AudioSearcher.Download, check duration mismatch
- Tag step: ID3v2.4 tag writing via Go library (e.g., `bogem/id3v2`)
- Store step: move file to permanent storage via AudioStore
- UpdateTrack step: transition Track to READY with audio_ref; rollback reverts to PENDING

**Patterns to follow:**
- Python pipeline: `services/api/src/altune/application/catalog/acquisition/pipeline.py`
- Python matching: `services/api/src/altune/application/catalog/acquisition/matching.py` — 3-tier selection with token matching
- Python steps: `services/api/src/altune/application/catalog/acquisition/steps/` — each step file

**Test scenarios:**
- Happy path: pipeline runs all 6 steps in order, track ends up READY with audio_ref
- Error path: pipeline rolls back completed steps in reverse when step 4 fails
- Happy path: matching selects Topic channel candidate when available
- Happy path: matching falls back to metadata rank when no Topic channel match
- Edge case: matching rejects candidates with >15% duration mismatch
- Happy path: search waterfall tries ISRC first, falls back through tiers
- Edge case: search deduplicates candidate URLs across waterfall tiers
- Error path: acquire marks track FAILED when no candidates found
- Integration: successful acquisition updates track in repository to READY status

**Verification:**
- Pipeline produces same acquisition results as Python for the same input track metadata
- Rollback correctly undoes partial progress on failure

---

- U9. **yt-dlp adapter and retry handler**

**Goal:** Port the yt-dlp subprocess wrapper and the retry-acquisition HTTP endpoint.

**Requirements:** R18, R6

**Dependencies:** U3, U8

**Files:**
- Create: `services/go-api/internal/acquisition/adapters/ytdlp/searcher.go`
- Create: `services/go-api/internal/acquisition/adapters/handler/retry_handler.go`
- Test: `services/go-api/internal/acquisition/adapters/ytdlp/searcher_test.go`
- Test: `services/go-api/internal/acquisition/adapters/handler/retry_handler_test.go`

**Approach:**
- `YtDlpAudioSearcher` implements `AudioSearcher` port. Uses `exec.CommandContext` with timeout for both search (`yt-dlp --dump-json "ytsearch5:<query>"`) and download (`yt-dlp -x --audio-format mp3`)
- Parse yt-dlp JSON output into AudioCandidate structs (title, artist, duration, url, channel, categories, view/follower counts)
- Download method: invoke yt-dlp with ffmpeg post-processor, temp directory, duration limit
- Retry handler: `POST /v1/tracks/{id}/retry-acquisition` — verify track is FAILED, schedule acquisition in goroutine

**Patterns to follow:**
- Python yt-dlp: `services/api/src/altune/adapters/outbound/audio/ytdlp_searcher.py` — search/download subprocess calls
- Python retry: `services/api/src/altune/adapters/inbound/http/catalog/router.py` retry_acquisition handler

**Test scenarios:**
- Happy path: search returns AudioCandidate list from yt-dlp JSON output
- Error path: search times out after configured duration → returns empty list
- Error path: yt-dlp binary not found → clear error message
- Happy path: download produces MP3 file at expected path
- Happy path: POST /v1/tracks/{id}/retry-acquisition for FAILED track → 202, acquisition scheduled
- Error path: POST /v1/tracks/{id}/retry-acquisition for READY track → 409 conflict

**Verification:**
- yt-dlp search returns candidates for known queries
- Download produces valid MP3 files
- Retry endpoint triggers new acquisition attempt

---

### Phase 4: Discovery Module

- U10. **Discovery domain and ports**

**Goal:** Port all discovery domain types and port interfaces to Go.

**Requirements:** R2, R3, R17

**Dependencies:** U1

**Files:**
- Create: `services/go-api/internal/discovery/domain/search_result.go`
- Create: `services/go-api/internal/discovery/domain/search_query.go`
- Create: `services/go-api/internal/discovery/domain/result_kind.go`
- Create: `services/go-api/internal/discovery/domain/confidence.go`
- Create: `services/go-api/internal/discovery/domain/source_ref.go`
- Create: `services/go-api/internal/discovery/domain/provider_status.go`
- Create: `services/go-api/internal/discovery/domain/provider.go`
- Create: `services/go-api/internal/discovery/domain/search_history.go`
- Create: `services/go-api/internal/discovery/domain/search_click.go`
- Create: `services/go-api/internal/discovery/domain/quality_score.go`
- Create: `services/go-api/internal/discovery/domain/entity_resolution_tier.go`
- Create: `services/go-api/internal/discovery/domain/content_validation_status.go`
- Create: `services/go-api/internal/discovery/domain/events.go`
- Create: `services/go-api/internal/discovery/ports/search_provider.go`
- Create: `services/go-api/internal/discovery/ports/query_cache.go`
- Create: `services/go-api/internal/discovery/ports/artwork.go`
- Create: `services/go-api/internal/discovery/ports/history_repo.go`
- Create: `services/go-api/internal/discovery/ports/click_repo.go`
- Create: `services/go-api/internal/discovery/ports/content_provider.go`
- Create: `services/go-api/internal/discovery/ports/mbid_resolver.go`
- Create: `services/go-api/internal/discovery/ports/fetch_success.go`
- Test: `services/go-api/internal/discovery/domain/search_query_test.go`
- Test: `services/go-api/internal/discovery/domain/confidence_test.go`
- Test: `services/go-api/internal/discovery/domain/entity_resolution_tier_test.go`

**Approach:**
- Direct 1:1 port of all Python domain types. Go structs for value objects, `iota` enums for enumerations
- SearchQuery: validation in constructor (non-empty raw, non-empty kinds, 1 ≤ limit ≤ 50)
- Confidence: comparable enum (HIGH > MEDIUM > LOW)
- EntityResolutionTier: comparable enum (MBID > ISRC > NONE)
- All port interfaces as Go interfaces matching Python Protocol signatures

**Patterns to follow:**
- Python domain: `services/api/src/altune/domain/discovery/` — all type definitions
- Python ports: `services/api/src/altune/application/discovery/ports.py` — interface signatures

**Test scenarios:**
- Happy path: SearchQuery validates with valid inputs
- Error path: SearchQuery rejects empty raw query
- Error path: SearchQuery rejects limit outside 1-50
- Happy path: Confidence comparison: HIGH > MEDIUM > LOW
- Happy path: EntityResolutionTier comparison: MBID > ISRC > NONE

**Verification:**
- All domain types compile and match Python field-for-field
- Port interfaces compile

---

- U11. **Provider adapters**

**Goal:** Port all 6 search provider adapters plus artwork resolvers (Deezer, MusicBrainz, iTunes, LastFm, SoundCloud, TheAudioDB, Wikidata, FanartTV, Genius).

**Requirements:** R19

**Dependencies:** U10

**Files:**
- Create: `services/go-api/internal/discovery/adapters/providers/deezer.go`
- Create: `services/go-api/internal/discovery/adapters/providers/musicbrainz.go`
- Create: `services/go-api/internal/discovery/adapters/providers/lastfm.go`
- Create: `services/go-api/internal/discovery/adapters/providers/soundcloud.go`
- Create: `services/go-api/internal/discovery/adapters/providers/itunes.go`
- Create: `services/go-api/internal/discovery/adapters/providers/theaudiodb.go`
- Create: `services/go-api/internal/discovery/adapters/providers/wikidata.go`
- Create: `services/go-api/internal/discovery/adapters/providers/genius.go`
- Create: `services/go-api/internal/discovery/adapters/providers/fanarttv.go`
- Create: `services/go-api/internal/discovery/adapters/providers/artwork_chain.go`
- Test: `services/go-api/internal/discovery/adapters/providers/deezer_test.go`
- Test: `services/go-api/internal/discovery/adapters/providers/musicbrainz_test.go`
- Test: `services/go-api/internal/discovery/adapters/providers/itunes_test.go`

**Approach:**
- Each adapter implements `SearchProvider` port interface using `net/http` client
- HTTP clients: one `*http.Client` per provider (bulkhead pattern, same as Python's per-provider AsyncClient)
- Deezer: free API, no auth. Maps JSON response to SearchResult
- MusicBrainz: requires User-Agent header. MBID-based dedup
- SoundCloud: yt-dlp based (subprocess, same as Python)
- LastFm: API key required. Also implements popularity resolution
- TheAudioDB: free API (key "123"). Also implements ArtworkResolver
- Artwork chain: tries resolvers in order (Deezer → TheAudioDB), skips Deezer placeholder
- Wikidata: SPARQL bridge for URL → MBID mapping
- FanartTV/Genius: artwork resolvers

**Patterns to follow:**
- Python adapters: `services/api/src/altune/adapters/outbound/discovery/` — each provider file

**Test scenarios:**
- Happy path: Deezer search returns SearchResult list with correct SourceRef
- Happy path: MusicBrainz search returns results with MBID in extras
- Happy path: iTunes search upscales artwork URL to 600x600
- Error path: provider returns non-200 → adapter returns empty results (not error)
- Edge case: SoundCloud adapter handles yt-dlp subprocess timeout
- Happy path: artwork chain tries Deezer first, falls back to TheAudioDB
- Edge case: artwork chain skips Deezer placeholder image

**Verification:**
- Each provider returns SearchResult objects matching Python adapter output format
- Artwork chain resolves images in the same priority order

---

- U12. **Redis cache adapters**

**Goal:** Port all 6 Redis-backed cache adapters for discovery.

**Requirements:** R8

**Dependencies:** U2, U10

**Files:**
- Create: `services/go-api/internal/discovery/adapters/cache/query_cache.go`
- Create: `services/go-api/internal/discovery/adapters/cache/artwork_cache.go`
- Create: `services/go-api/internal/discovery/adapters/cache/mbid_cache.go`
- Create: `services/go-api/internal/discovery/adapters/cache/popularity_cache.go`
- Create: `services/go-api/internal/discovery/adapters/cache/content_validation.go`
- Create: `services/go-api/internal/discovery/adapters/cache/fetch_success.go`
- Test: `services/go-api/internal/discovery/adapters/cache/query_cache_test.go`
- Test: `services/go-api/internal/discovery/adapters/cache/fetch_success_test.go`

**Approach:**
- Each cache implements its port interface from U10
- Key formats: preserve Python's key patterns (e.g., `discovery:v1:{provider}:{kinds}:{hash}`) for backward compatibility during migration — both Python and Go may read the same Redis during the transition
- TTLs: same as Python (query cache per-source, artwork 14d positive / 24h negative, MBID 30d, etc.)
- All caches degrade gracefully on Redis error (return miss, not error)
- FetchSuccessStore: sliding-window success rate stored as Redis list (last 10 entries)

**Patterns to follow:**
- Python caches: `services/api/src/altune/adapters/outbound/discovery/cache/` — key formats, TTLs, degradation

**Test scenarios:**
- Happy path: query cache stores and retrieves per-source results
- Happy path: query cache returns miss after TTL expires
- Edge case: query cache degrades gracefully on Redis error
- Happy path: fetch success store computes rate from sliding window
- Happy path: artwork cache stores positive (14d) and negative (24h) results with different TTLs

**Verification:**
- Cache keys match Python format (can read Python-written cache entries)
- TTLs match Python configuration

---

- U13. **Discovery engine (dedup, ranking, circuit breaker, quality scorer)**

**Goal:** Port the core discovery algorithms: identifier-only merge, RRF scoring, per-source circuit breaker, quality scoring, and normalization.

**Requirements:** R19

**Dependencies:** U10

**Files:**
- Create: `services/go-api/internal/discovery/service/normalize.go`
- Create: `services/go-api/internal/discovery/service/dedup.go`
- Create: `services/go-api/internal/discovery/service/circuit_breaker.go`
- Create: `services/go-api/internal/discovery/service/quality_scorer.go`
- Create: `services/go-api/internal/discovery/service/url_router.go`
- Test: `services/go-api/internal/discovery/service/normalize_test.go`
- Test: `services/go-api/internal/discovery/service/dedup_test.go`
- Test: `services/go-api/internal/discovery/service/circuit_breaker_test.go`
- Test: `services/go-api/internal/discovery/service/quality_scorer_test.go`

**Approach:**
- Normalize: 8-step canonicalization (NFKC, lowercase, diacritics, articles, brackets, features, punctuation, whitespace) — direct port using Go's `unicode` and `regexp` packages
- Dedup: identifier-only merge (ISRC/MBID). Ranking: relevance → demotion → quality → popularity → RRF → alpha. Same algorithm as Python's `fuse_and_rank`
- Circuit breaker: per-source 3-state (CLOSED → OPEN after 5 failures → HALF_OPEN after 30s). In-memory, same as Python
- Quality scorer: composite from completeness, agreement, entity_resolution_tier, fetch_success_rate. Demotion for non-canonical record types
- URL router: regex matching for Deezer/MusicBrainz/SoundCloud/LastFm URLs

**Patterns to follow:**
- Python dedup: `services/api/src/altune/application/discovery/dedup.py` — merge and ranking algorithm
- Python circuit breaker: `services/api/src/altune/application/discovery/circuit_breaker.py`
- Python normalize: `services/api/src/altune/application/discovery/normalize.py`
- Python quality: `services/api/src/altune/application/discovery/quality_scorer.py`

**Test scenarios:**
- Happy path: normalize produces same output as Python for test strings
- Happy path: dedup merges two results with matching ISRC into one with multiple SourceRefs
- Happy path: dedup merges two results with matching MBID
- Edge case: dedup does NOT merge results without shared identifiers (no text similarity)
- Happy path: RRF ranking produces expected order for known input set
- Happy path: circuit breaker opens after 5 consecutive failures
- Happy path: circuit breaker transitions to HALF_OPEN after 30s
- Happy path: quality score composite calculation matches Python output
- Happy path: URL router detects Deezer, MusicBrainz, SoundCloud URLs

**Verification:**
- Dedup + ranking produces identical output to Python for the same input set
- Normalization produces identical output to Python for test corpus

---

- U14. **Discovery use cases and HTTP handlers**

**Goal:** Port search_music (scatter-gather), record_click, list_history, get_album_tracks, get_artist_content use cases and the discovery HTTP handler.

**Requirements:** R6, R19

**Dependencies:** U3, U10, U11, U12, U13

**Files:**
- Create: `services/go-api/internal/discovery/service/search_music.go`
- Create: `services/go-api/internal/discovery/service/record_click.go`
- Create: `services/go-api/internal/discovery/service/list_history.go`
- Create: `services/go-api/internal/discovery/service/get_album_tracks.go`
- Create: `services/go-api/internal/discovery/service/get_artist_content.go`
- Create: `services/go-api/internal/discovery/adapters/handler/discovery_handler.go`
- Create: `services/go-api/internal/discovery/adapters/persistence/queries/history.sql`
- Create: `services/go-api/internal/discovery/adapters/persistence/queries/clicks.sql`
- Create: `services/go-api/internal/discovery/adapters/persistence/sqlc.yaml`
- Create: `services/go-api/internal/discovery/adapters/persistence/history_repo.go`
- Create: `services/go-api/internal/discovery/adapters/persistence/click_repo.go`
- Test: `services/go-api/internal/discovery/service/search_music_test.go`
- Test: `services/go-api/internal/discovery/adapters/handler/discovery_handler_test.go`
- Test: `services/go-api/internal/discovery/adapters/persistence/history_repo_test.go`

**Approach:**
- SearchMusic: scatter-gather across providers using goroutines + `errgroup` (replaces Python's asyncio.gather). Per-source circuit breaker check. Query cache check per source. Merge via dedup engine. Enrich (popularity + artwork). Rank. Persist history. Return results + provider statuses
- RecordClick: compute result_signature (sha256), insert with sliding-window dedup
- ListHistory: distinct-recent (group by query_norm, latest per group, limit N)
- GetAlbumTracks/GetArtistContent: single-provider fetch, record validation outcome
- HTTP handler: routes match Python's discovery router paths exactly
- Persistence: sqlc for search_history and search_clicks tables (ring buffer trim, sliding-window dedup)

**Patterns to follow:**
- Python search: `services/api/src/altune/application/discovery/search_music.py` — scatter-gather, cache, dedup, rank, enrich
- Python click: `services/api/src/altune/application/discovery/record_click.py` — sha256 signature, dedup window
- Python history: `services/api/src/altune/application/discovery/list_search_history.py`
- Python handler: `services/api/src/altune/adapters/inbound/http/discovery/router.py`

**Test scenarios:**
- Happy path: search returns merged, ranked results from multiple providers
- Happy path: search uses cache for recently-queried providers
- Edge case: search returns partial results when some providers fail (circuit breaker open)
- Happy path: search persists history entry
- Happy path: record click with valid result_signature → inserted
- Edge case: record click within dedup window → not inserted
- Happy path: list history returns distinct-recent queries
- Happy path: GET /v1/discovery/search returns DiscoverySearchResponse with results + provider statuses
- Happy path: GET /v1/discovery/albums/{provider}/{id}/tracks returns track list

**Verification:**
- Search returns results matching Python API response format
- Provider scatter-gather completes within reasonable timeout
- All discovery endpoints match Python paths and response schemas

---

### Phase 5: Integration

- U15. **CLI commands**

**Goal:** Port operational CLI commands (dedup migration, health check, fix audio refs) as subcommands of the Go binary.

**Requirements:** R17

**Dependencies:** U5, U7

**Files:**
- Modify: `services/go-api/cmd/api/main.go` (add cobra subcommands)
- Create: `services/go-api/cmd/api/commands/dedup_migration.go`
- Create: `services/go-api/cmd/api/commands/health_check.go`
- Create: `services/go-api/cmd/api/commands/fix_audio_refs.go`
- Test: `services/go-api/cmd/api/commands/dedup_migration_test.go`

**Approach:**
- Use cobra for subcommand structure: `go-api serve` (default), `go-api migrate-dedup [--execute]`, `go-api health-check [--fix]`, `go-api fix-audio-refs [--execute]`
- Dedup migration: identify tracks with matching (user_id, dedup_key), rank by status/date, keep one, remap playlists. Dry-run by default
- Health check: compare DB tracks with audio files in Object Storage, report orphans. `--fix` marks broken tracks as failed
- Fix audio refs: strip UUID prefix from legacy audio_ref paths

**Patterns to follow:**
- Python CLI: `services/api/src/altune/adapters/inbound/cli/` — dedup_migration.py, health_check.py, fix_audio_refs.py

**Test scenarios:**
- Happy path: dedup migration dry-run identifies duplicates without modifying data
- Happy path: dedup migration --execute removes duplicates and remaps playlists
- Happy path: health check reports orphaned tracks
- Error path: health check --fix marks tracks with missing audio as FAILED

**Verification:**
- CLI commands produce same output as Python equivalents for the same data

---

- U16. **Composition root and wiring**

**Goal:** Wire all modules together in the composition root. Connect ports to adapters, mount routes, manage lifecycle.

**Requirements:** R1, R14

**Dependencies:** U3, U6, U7, U9, U14, U15

**Files:**
- Create: `services/go-api/internal/app/app.go`
- Modify: `services/go-api/cmd/api/main.go`

**Approach:**
- `app.go` is the composition root: creates config → database pool → Redis client → auth middleware → module services (injecting adapters into port interfaces) → chi router with all routes mounted → HTTP server with graceful shutdown
- Module wiring mirrors Python's `platform/app.py` and `platform/wiring.py`: construct providers with per-provider HTTP clients, build discovery caches (if Redis available), chain artwork resolvers, wire audio store (OCI > filesystem priority)
- Server lifecycle: graceful shutdown on SIGINT/SIGTERM, close DB pool, close Redis, close HTTP clients
- Route mounting: catalog routes at `/v1/tracks` and `/v1/playlists`, discovery routes at `/v1/discovery`, health at `/health`

**Patterns to follow:**
- Python wiring: `services/api/src/altune/platform/app.py` — lifespan context, state wiring
- Python wiring: `services/api/src/altune/platform/wiring.py` — provider construction, cache wiring

**Test scenarios:**
- Happy path: composition root starts with all config fields set — all routes mounted, all adapters non-nil
- Edge case: composition root with Redis unavailable — caches degrade, server still starts
- Edge case: composition root without FanartTV API key — artwork chain excludes FanartTV resolver
- Edge case: composition root without Last.fm API key — popularity resolver not wired
- Edge case: composition root without MusicBrainz User-Agent — MB provider excluded from discovery

**Verification:**
- `go run ./cmd/api serve` starts the server with all routes mounted
- All endpoints respond (verified in U17)

---

- U17. **Docker Compose deployment and end-to-end verification**

**Goal:** Deploy the Go binary via Docker Compose on OCI instance. Verify all endpoints match Python behavior.

**Requirements:** R14, R15, R16

**Dependencies:** U16

**Files:**
- Modify: `docker-compose.yml` (finalize production config)
- Modify: `services/go-api/Dockerfile` (finalize if needed)
- Modify: `services/go-api/.env.example` (complete with all required vars)

**Approach:**
- docker-compose.yml: Go API service builds from `services/go-api/Dockerfile`, exposes port 8000, env vars from `.env`. Postgres service (or external Supabase URL). Redis service
- Deploy to OCI: `docker compose up -d` on the OCI instance
- End-to-end verification: point mobile app at Go endpoint, test all flows (library browsing, search, playlists, streaming, acquisition retry)

**Test scenarios:**
- Covers AE1. Integration: mobile app lists tracks from Go backend
- Covers AE1. Integration: mobile app searches and gets discovery results from Go backend
- Covers AE1. Integration: mobile app streams audio through Go backend
- Covers AE1. Integration: mobile app creates/manages playlists through Go backend
- Integration: Docker Compose starts all 3 services cleanly
- Integration: Go API connects to Supabase Postgres and Redis
- Integration: health endpoint returns `{"db": "ok"}`

**Verification:**
- All mobile app features work identically against Go backend as they did against Python
- Docker Compose deployment runs stable on OCI instance
- No regressions visible in mobile app behavior

---

## System-Wide Impact

- **Interaction graph:** Auth middleware intercepts all `/v1/*` routes. Background acquisition goroutines run independently of HTTP request lifecycle. Discovery scatter-gather spawns concurrent goroutines per provider. Cache adapters degrade silently on Redis failure.
- **Error propagation:** Domain errors (NotFound, Unauthorized, ValidationError) map to HTTP status codes in `httputil/errors.go`. Provider errors are caught per-provider and surfaced as ProviderStatus (not propagated to caller). Audio file missing triggers Fix-Log-Signal cascade (same as Python).
- **State lifecycle risks:** Background acquisition goroutines must not outlive server shutdown — use context cancellation. Redis cache keys must match Python format during transition period. Database connection pool must handle Supabase's connection limits.
- **API surface parity:** All `/v1/*` endpoints produce identical JSON responses. Status codes match. Auth behavior matches. Error response format matches.
- **Unchanged invariants:** Mobile client (`apps/mobile/`) is unchanged. Database schema is unchanged. OCI Object Storage bucket and key format are unchanged. Supabase Auth configuration is unchanged.

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Go fuzzy matching library doesn't match Python's `token_sort_ratio` behavior | Test with known input/output pairs from Python; fall back to porting the algorithm directly |
| sqlc-generated code doesn't handle Postgres-specific types (arrays, UUIDs) cleanly | pgx v5 has native UUID/array support; test early in U5 |
| yt-dlp/ffmpeg subprocess behavior differs on Linux (OCI) vs Windows (dev) | Test subprocess calls on both platforms; use Docker for consistent dev environment |
| Discovery scatter-gather goroutine leak on timeout | Use `errgroup` with context cancellation; add timeout per provider |
| Redis key format mismatch breaks cache during migration transition | Preserve Python key format exactly; verify with integration test reading Python-written cache |
| Supabase JWKS endpoint format changes or rate limits | Cache JWKS with auto-refresh (jwx handles this); test with real Supabase tokens |

---

## Sources & References

- **Origin document:** [docs/brainstorms/2026-06-14-go-modular-monolith-requirements.md](docs/brainstorms/2026-06-14-go-modular-monolith-requirements.md)
- Related code: `services/api/src/altune/` (entire Python backend being ported)
- Related ADR: `docs/adr/0002-stack-expo-fastapi.md` (superseded by this migration)
- Institutional learning: `docs/solutions/design-patterns/2026-06-08-combined-identity-string-matching-over-field-gates.md`
- Institutional learning: `docs/solutions/2026-06-10-type-checking-import-runtime-crash.md`
