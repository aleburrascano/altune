# Architecture

High-level shape of the Altune codebase. Pair with `docs/ubiquitous-language.md` for terminology and the per-feature/per-context nested `CLAUDE.md` files for local rules.

## Top-level layout

```
altune/
├── apps/
│   └── mobile/        # Expo (React Native + TypeScript) — vertical-slice feature folders
└── services/
    └── api/           # Python (FastAPI) — hexagonal architecture (Ports & Adapters)
```

There is intentionally **one** mobile client and **one** API service today. New clients (web, desktop) or services (workers, ingestion) come with an ADR and a new top-level folder.

## Backend (`services/api/`) — hexagonal architecture

Reference: [vault: wiki/concepts/Hexagonal Architecture.md].

```
services/api/src/altune/
├── domain/        # pure business model. zero framework dependencies.
├── application/   # use cases + port interfaces (the application's API to itself)
├── adapters/
│   ├── inbound/   # things that DRIVE the app (HTTP routers, CLI, message consumers)
│   └── outbound/  # things the app DRIVES (DB repositories, external HTTP clients, message publishers)
└── platform/      # config, DI wiring, logging, observability infra
```

### Dependency rule (the load-bearing rule)

Dependencies point **inward only**:

```
adapters → application → domain
platform → everything (it wires)
```

- `domain/` imports nothing from `adapters/` or framework code.
- `application/` imports `domain/` + standard library. **Ports** are defined here. Adapters implement them.
- `adapters/` imports `application/` ports + `domain/` types + framework code.

Path-scoped rules in `.claude/rules/domain-layer.md`, `application-layer.md`, `adapters-layer.md` enforce this; the `architecture-reviewer` subagent grades against it.

### Bounded contexts

Features inside the backend organize by **bounded context** (a DDD strategic concept — [vault: wiki/concepts/Bounded Context.md]). A bounded context is a cohesive area of the domain with consistent terminology.

Initial expected contexts (will materialize as features land):
- `catalog/` — tracks, artists, albums; the immutable identity-and-metadata side
- `library/` — user's personal collection (favorites, custom playlists, play counts)
- `playback/` — runtime: queue, current track, scrubbing, history
- `metadata/` — external enrichment (album art, lyrics, bios — pulled from third-party APIs)

These are not created until they have features driving them. Day-1 scaffolding has only the layer dirs.

### Per-context layout

When a context is added:

```
src/altune/
├── domain/<context>/
│   ├── __init__.py
│   ├── <aggregate>.py
│   ├── <value-objects>.py
│   ├── events.py
│   └── exceptions.py
├── application/<context>/
│   ├── __init__.py
│   ├── ports.py           # interfaces consumed by use cases here
│   └── <use-case>.py      # one file per use case
└── adapters/
    ├── inbound/http/<context>/
    │   └── router.py
    └── outbound/persistence/<context>/
        └── <aggregate>_repository.py
```

## Frontend (`apps/mobile/`) — vertical-slice architecture

Reference: [vault: wiki/concepts/Vertical Slice Architecture.md].

```
apps/mobile/src/
├── app/                       # Expo Router root (file-based routes)
├── features/                  # one folder per feature; vertical slice
│   ├── _template/             # the shape every new feature follows
│   ├── <feat>/
│   │   ├── CLAUDE.md          # feature-local context
│   │   ├── ui/                # screens + feature components
│   │   ├── hooks/             # feature hooks
│   │   ├── api/               # client calls (typed via shared/api-client)
│   │   ├── types.ts
│   │   └── __tests__/
│   └── ...
└── shared/                    # used by 2+ features ONLY
    ├── ui/                    # design system (tokens, primitives)
    ├── api-client/            # generated typed HTTP client + interceptors
    └── lib/                   # pure utilities
```

### The slice rule

A feature folder owns everything for that feature **end-to-end**: UI, hooks, API calls, tests. Cross-feature imports are forbidden — extraction to `shared/` requires 2+ real consumers. The `architecture-reviewer` enforces this.

## Cross-cutting

### Authentication / authorization
Deferred until first feature needs it. When chosen → ADR + auth bounded context in backend + `shared/api-client/` integration on mobile.

### Persistence
Deferred until first feature needs it. SQLite or Postgres TBD via `/brainstorm-tech-choice` + ADR. Adapter lives under `adapters/outbound/persistence/`.

### Observability
- Structured logging (`structlog`) in backend; correlation id per request.
- Sentry / equivalent for crash reporting: TBD via ADR when shipping.
- Metrics: TBD; not blocking pre-launch.

### Configuration
- 12-factor: env vars in production, `.env` for local dev (gitignored), `.env.example` checked in as documentation.
- Loaded via Pydantic Settings in `platform/config.py`.

### Inter-service communication
Single service today. When a second service is introduced (worker, ingestion) → ADR for transport (HTTP, queue, etc.) + an outbound adapter on the caller and inbound adapter on the callee.

## Quality attributes (priority order)

When trade-offs arise, decide in this order:

1. **Correctness** — does it actually do the right thing? (Tests, vault-pattern alignment.)
2. **Maintainability** — can future-me change it without rework? (Hexagonal layers, vertical slices, ubiquitous language consistency.)
3. **Testability** — can we test without I/O? (In-memory adapter implementations of ports.)
4. **Observability** — when production breaks, can we diagnose? (Structured logs, correlation ids.)
5. **Performance** — at the scale we expect, is it fast enough? (Per-request N+1 awareness; mobile render perf.)
6. **Resilience** — does the right thing happen on partial failure? (Retries, idempotency where applicable; covered when features need it.)

Order matters: don't optimize for performance at the cost of correctness or maintainability.

## Workflow

The implementation discipline is in `docs/workflows/new-feature.md`. Read it.

## Decisions and learnings

- Architectural decisions live in `docs/adr/`. Status transitions: Proposed → Accepted → Deprecated/Superseded. Never delete.
- Compound learnings (patterns discovered, not bug instances) live in `docs/solutions/`. Auto-captured by `/compound-learning`; periodically consolidated by `/audit-docs`.
- Brainstorms (option-weighing not yet committed) live in `docs/brainstorms/` and auto-prune at 30 days untouched + not graduated.

## See also

- `docs/workflows/new-feature.md` — the feature loop
- `docs/ubiquitous-language.md` — domain glossary
- `docs/claude-md-map.md` — index of all CLAUDE.md files in the repo
- `.claude/rules/` — path-scoped rules enforcing architecture decisions
- `[vault: wiki/concepts/Hexagonal Architecture.md]`, `[vault: wiki/concepts/Vertical Slice Architecture.md]`, `[vault: wiki/concepts/Domain-Driven Design.md]`
