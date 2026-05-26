# ADR-0002: Stack — Expo (RN + TS) mobile, Python (FastAPI) backend, hexagonal architecture

- **Status:** Accepted
- **Date:** 2026-05-25
- **Deciders:** solo + Claude
- **Context tags:** [tech-stack, arch]

## Context

Altune is a solo-built music manager intended for production-grade quality. The stack choice needs to:

- Match the developer's existing strength (Python familiar; TS competent).
- Enable a mobile-first UX without doubling implementation effort for iOS/Android.
- Support strict type safety + testability on both sides.
- Have mature Claude-ecosystem tooling (skills/agents/hooks references in the vault).
- Avoid lock-in for the persistence/auth choices (decided later via separate ADRs).

## Decision

**Mobile:** Expo (React Native + TypeScript, strict). Routing via Expo Router. State: React Query (server) + hooks (local). No global state library until ADR justifies one.

**Backend:** Python 3.12+, FastAPI, **hexagonal architecture** (Ports & Adapters). Layers: `domain` / `application` / `adapters` / `platform`. Type-strict via mypy. Tooling: `uv` for env/deps, `ruff` for lint+format, `pytest` for tests, `testcontainers` for integration.

**Cross-cutting:** Conventional Commits + commitlint + AI-attribution-strip in the commit-msg hook. Documentation discipline per `docs/workflows/new-feature.md`.

## Alternatives considered

### Mobile

| Option | Why not |
|---|---|
| **React Native (bare workflow, no Expo)** | More config; loses Expo's OTA updates, dev client, and managed services. Bare can come later if needed (Expo supports prebuild). |
| **Native iOS + Native Android** | Solo: doubles implementation. Disqualified. |
| **Flutter** | Dart unfamiliar; smaller TS-equivalent ecosystem; less Claude-vault coverage. |
| **PWA / Capacitor** | Audio playback + background behavior on mobile is weaker than native. |

### Backend

| Option | Why not |
|---|---|
| **Node.js / TypeScript (Fastify or Hono)** | Would unify language with mobile, but Python's audio/metadata ecosystem (mutagen, librosa, etc.) is materially better for a music app, and the dev's Python is stronger. |
| **Go** | Type safety + concurrency are great; ecosystem for music metadata weaker; dev not idiomatic. |
| **Django + DRF** | Heavier; ORM-coupling tighter; hexagonal layering fights Django's conventions. |
| **Flat FastAPI without hexagonal** | Works for prototypes; collapses into mud at scale. We're optimizing for long-lived enterprise-grade. |

### Architecture

| Option | Why not |
|---|---|
| **Layered (Controller → Service → Repository) without ports** | Couples application to specific repository implementation; testing requires DB. Hexagonal gives the port seam for free. |
| **Clean Architecture (formalized)** | Effectively the same shape as hexagonal at this scale; the explicit "use cases" + "interface adapters" layers add ceremony without benefit for a solo project today. Easy to graduate from hexagonal to Clean if it ever matters. |
| **Onion** | Same family as hexagonal; we pick hexagonal naming because the vault note is more thorough and the port/adapter vocabulary maps cleanly to FastAPI's DI. |

## Consequences

### What becomes easier
- Strict typing on both sides → less runtime surprise; mypy and tsc carry weight.
- Hexagonal: domain + application testable without DB or HTTP (in-memory adapters in tests).
- Expo: single codebase iOS/Android, fast dev cycles, mature OTA story.
- Python ecosystem for audio metadata / external API integrations is robust.

### What becomes harder
- **Two runtimes to maintain** — must keep both Node and Python toolchains current.
- **No shared type system** — backend Pydantic models and frontend TS types are mirrors maintained manually (until/if we adopt OpenAPI codegen, which is its own ADR).
- **Mobile bridging perf considerations** — RN's JS bridge can be a bottleneck for audio-heavy work; deferred to a performance ADR if it actually bites.

### What we're committing to (and the cost to reverse)
- **Expo** — swapping to bare React Native: contained (prebuild). Swapping to a different framework: large (rewrite mobile).
- **Python + FastAPI** — swapping to another Python framework: moderate (rewrite the inbound HTTP adapter; domain/application untouched). Swapping language: full rewrite.
- **Hexagonal** — committed for the lifetime of the backend; relaxing it later means rewriting around a different shape, which is much worse than tightening it as we go.

## Vault references

- [vault: wiki/concepts/Hexagonal Architecture.md] — the chosen backend architecture
- [vault: wiki/concepts/Vertical Slice Architecture.md] — the chosen mobile organization
- [vault: wiki/concepts/Domain-Driven Design.md] — informs the domain layer's tactical patterns
- [vault: wiki/concepts/Repository Pattern.md] — for outbound persistence adapters
- [vault: wiki/concepts/Dependency Injection.md] — for wiring ports to adapters

## Deferred decisions (separate ADRs needed when triggered)

- ADR-NNNN: **persistence** — Postgres vs. SQLite vs. cloud-managed. Trigger: first feature needing durable state.
- ADR-NNNN: **authentication** — Supabase vs. FastAPI-Users vs. external IdP. Trigger: first feature needing user identity.
- ADR-NNNN: **background work** — async tasks vs. Celery vs. simple queue. Trigger: first feature needing scheduled or deferred work.
- ADR-NNNN: **mobile state library** — React Query alone vs. + Zustand/Jotai. Trigger: shared client state that hooks can't handle cleanly.
- ADR-NNNN: **OpenAPI codegen** for typed mobile client. Trigger: drift between backend Pydantic and frontend TS types becomes painful.

## Related

- ADR-0001 — monorepo layout
- Architecture: `docs/architecture.md`
