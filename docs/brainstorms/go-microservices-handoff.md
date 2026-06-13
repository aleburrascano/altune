# Handoff: Go Microservices Migration + OCI Object Storage

## Context

Altune is a self-hosted music manager with an Expo (React Native) mobile client and a Python (FastAPI) hexagonal backend. The codebase is at `C:\Users\Alessandro\Desktop\altune`. It currently runs as a monolith with all bounded contexts (catalog, discovery, playback) in one Python process.

## Current state

- **Backend**: Python 3.12, FastAPI, hexagonal architecture, SQLAlchemy async + asyncpg, Supabase auth (JWT).
- **Audio storage**: 1,595 mp3 files on an OCI instance at `151.145.41.81` (SSH user: `ubuntu`, key: `~/.ssh/oci_music_manager.key`). Files at `/mnt/music/aleburrascano123@gmail.com/{artist}/{album}/{track}.mp3`. Currently served via an `SshAudioStore` adapter that fetches files over SCP on demand (temp-file approach — works but not production-grade).
- **Database**: Supabase-hosted Postgres. Tracks have `audio_ref` with relative paths like `SLAYR/Half Blood (BloodLuxe)/Toxic.mp3`.
- **Resilience**: A `resilience-v1` spec was shipped with cascade-on-failure patterns, health-check CLI, dedup migration CLI. See `docs/specs/resilience-v1/spec.md` and `docs/patterns/data-consistency.md`.

## What needs to happen

### 1. OCI Object Storage migration

Move audio files from the block volume (`/mnt/music/`) to OCI Object Storage (S3-compatible). This decouples storage from the compute instance and enables any service to access audio via HTTPS.

- Create an OCI Object Storage bucket (e.g., `altune-audio`)
- Migrate 1,595 files from `/mnt/music/aleburrascano123@gmail.com/` to the bucket, preserving the relative path as the object key
- Replace `SshAudioStore` with an `ObjectStorageAudioStore` that uses the S3-compatible API (Oracle's Object Storage supports S3 via `aws-sdk` with a custom endpoint)
- For streaming: generate pre-signed URLs (client streams directly from Object Storage) OR proxy through the API
- Update `audio_ref` if key format changes (currently relative paths — should work as-is for object keys)

### 2. Go microservices (Strangler Fig pattern)

Incrementally replace the Python monolith with Go services. See `[vault: wiki/concepts/Strangler Fig Pattern.md]` for the pattern.

**Proposed service boundaries:**

| Service | Responsibility | Current location in Python |
|---------|---------------|---------------------------|
| `catalog-svc` | Track CRUD, playlists, dedup, library queries | `domain/catalog/`, `application/catalog/`, `adapters/.../catalog/` |
| `discovery-svc` | Search, provider scatter-gather, ranking | `domain/discovery/`, `application/discovery/`, `adapters/.../discovery/` |
| `streaming-svc` | Audio streaming, pre-signed URL generation | `stream_audio` handler in catalog router |
| `acquisition-svc` | yt-dlp download, audio processing, upload to Object Storage | `application/catalog/acquisition/`, `adapters/outbound/audio/` |
| `gateway` | API routing, auth (JWT verification), rate limiting | Currently in FastAPI middleware |

**Migration order (suggested):**
1. `streaming-svc` — smallest, most isolated, highest performance need (Go excels here)
2. `acquisition-svc` — CPU-bound (yt-dlp, ffmpeg), benefits from Go's concurrency
3. `catalog-svc` — core CRUD, straightforward port
4. `discovery-svc` — most complex (multi-provider, ranking), port last
5. `gateway` — once all services exist, add a routing layer

**Infrastructure needed:**
- Docker + Kubernetes (user plans to deploy on OCI)
- Service mesh or API gateway for inter-service routing
- Message broker (NATS or Redis Pub/Sub) for domain events between services
- Each service gets its own repo directory or Git repo
- CI/CD pipeline per service

### 3. Data consistency in microservices

The `docs/patterns/data-consistency.md` contract applies across services. In the monolith, cascade events are in-process function calls. In microservices, they become messages on a broker. The pattern is the same; the transport changes.

## OCI instance details

- **Public IP**: `151.145.41.81`
- **SSH**: `ssh -i ~/.ssh/oci_music_manager.key ubuntu@151.145.41.81`
- **Music files**: `/mnt/music/{user_email}/{artist}/{album}/{track}.mp3`
- **Mount**: block volume at `/mnt/music`, currently NFS-exported to a Raspberry Pi (Tailscale, can be deprecated)
- **Old project** (reference): `C:\Users\Alessandro\music-manager` — Flask-based, used yt-dlp pipeline for downloads, same Supabase for auth

## Key files to read

- `docs/architecture.md` — hexagonal architecture overview
- `docs/specs/resilience-v1/spec.md` — data consistency spec
- `docs/patterns/data-consistency.md` — cascade contract
- `services/api/src/altune/adapters/outbound/audio/ssh_store.py` — current SSH store (to be replaced)
- `services/api/src/altune/adapters/outbound/audio/filesystem_store.py` — local store (reference)
- `services/api/src/altune/application/catalog/ports.py` — AudioStore protocol
- `services/api/src/altune/platform/app.py` — app startup wiring
- `commitlint.config.js` — valid commit scopes
