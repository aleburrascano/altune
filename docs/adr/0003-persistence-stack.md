# ADR-0003: Persistence — SQLAlchemy 2.0 async + asyncpg + Alembic; Postgres in Docker (dev) / new Supabase project (prod)

- **Status:** Accepted
- **Date:** 2026-05-26
- **Deciders:** solo + Claude
- **Context tags:** [tech-stack, dependency, layer]

## Context

ADR-0002 deferred the persistence decision until the first feature requiring durable state. That feature is now imminent: `view-library` will list tracks from the user's library, and the walking-skeleton pre-slice that precedes it needs a chosen DB driver, an explicit position on ORM vs raw driver, a migration tool, and dev/prod hosting.

Constraints driving this decision:
- Hexagonal layering — persistence code is confined to `adapters/outbound/persistence/`; the domain must not import the chosen library.
- Async-first I/O — backend `services/api/CLAUDE.md` already names asyncpg/aiosqlite as the family of acceptable drivers.
- `mypy --strict` is enforced on every layer.
- The old `music-manager` project is still in active personal use on its existing Supabase instance, and must not be disturbed.
- Solo developer; the prior project's collapse traces to vendor coupling and ad-hoc data access — the new stack must keep the application code vendor-neutral.

The four candidate stacks were evaluated in `docs/brainstorms/2026-05-26-persistence-stack.md` (weighted matrix: SQLAlchemy 2.0 + asyncpg + Alembic = 76, asyncpg-direct + yoyo = 64, Tortoise + Aerich = 60, supabase-py = 52).

## Decision

Adopt **SQLAlchemy 2.0 async + asyncpg + Alembic** as the persistence stack.

The repository adapter is implemented per the example already codified in `.claude/rules/adapters-layer.md`: an `async def` repository class that holds an `AsyncSession`, owns the SQL↔domain mapping, and returns domain objects to the application layer. `async_sessionmaker(engine, expire_on_commit=False)` is the canonical session factory. Alembic uses the async `env.py` template (`async_engine_from_config` + `asyncio.run(run_async_migrations())`) so migrations and runtime code share one driver.

**Dev hosting:** local Postgres 16 in Docker via a `docker-compose.yml` at the repo root. Integration tests under `tests/integration/` spin ephemeral Postgres instances via `testcontainers-python` (already declared in `pyproject.toml` dev extras).

**Prod hosting:** a **new** Supabase project, separate from the old `music-manager` Supabase. The application connects via `DATABASE_URL` only — no `supabase-py` SDK, no PostgREST. Supabase's auth and storage features are NOT used in v1; they get their own ADRs if and when adopted.

## Alternatives considered

| Alternative | Why not |
|---|---|
| **asyncpg direct (no ORM) + yoyo-migrations** | Migration tool gap — `.claude/rules/migrations.md` already names Alembic; yoyo would force a rule rewrite. We'd also reinvent unit-of-work, bulk-insert ergonomics, and identity-map semantics by hand. Keep it as the fallback if SQLAlchemy ever proves too heavy. |
| **supabase-py (PostgREST HTTP client)** | Couples the entire repository layer to Supabase's HTTP API and dashboard-driven schema management. The hexagonal "swap the adapter" benefit evaporates because the repository implementation *is* Supabase verbs. Also adds an HTTP hop where there should be a direct DB connection. This is the failure mode that killed the old project. |
| **Tortoise ORM + Aerich** | Smaller community than SQLAlchemy; the vault has no notes citing it; no rule file anticipates its shape. Active-record-flavored models fight DDD aggregate invariants. We'd be the test case for the toolchain. |

## Consequences

### What becomes easier
- The domain and application layers are testable with an `InMemoryTrackRepository` and no real DB.
- Integration tests spin a real Postgres in CI (testcontainers) and exercise the real adapter — catching ORM mapping bugs that unit tests can't, per [vault: wiki/concepts/Integration Testing.md].
- Swapping prod hosting (Supabase → managed Postgres → self-hosted) is a connection-string change. No application code moves.
- The first feature's adapter matches the example in `.claude/rules/adapters-layer.md` line-for-line — Claude can pattern-match the shape reliably in future sessions.
- Alembic's async `env.py` template is well-documented; `uv run alembic init -t async migrations` scaffolds it.

### What becomes harder
- Boilerplate: every repository writes its own `Row ↔ Domain` mapping. We accept this as the price of keeping the domain pure.
- Alembic discipline — migrations must be reversible (`downgrade()` defined), idempotent on dev resets, and never edited after merging (per `.claude/rules/migrations.md`).
- Docker becomes a dev prerequisite — testcontainers needs Docker Desktop running on Windows. If Docker isn't available the integration test suite cannot run.
- Async-aware ORM has sharper edges than sync (no implicit lazy loads; must use `selectinload`/`AsyncAttrs`). The discipline is documented in SQLAlchemy 2.0's async tutorial and is now a rule the codebase commits to.

### What we're committing to (and the cost to reverse)
- **Postgres-flavored SQL** — switching engines (e.g., to SQLite for an offline-first variant) is moderate: SQLAlchemy abstracts the dialect, but Alembic migrations and any future Postgres-specific features (JSONB, partial indexes, `pgvector`) become engine-coupled. Reversal cost: rewrite migrations, drop Postgres-only types.
- **Alembic as the migration tool** — for the lifetime of the project. Swapping migration tools later is painful because the history is a sequence of Python files Alembic owns. Effectively irreversible.
- **Docker for dev integration tests** — anyone working on the codebase needs Docker Desktop (Windows/macOS) or a Docker daemon (Linux). Reversal cost: rewrite the integration-test setup against an embedded SQLite (loses dialect fidelity).
- **Supabase as the prod host** — contained at the connection-string boundary. Reversal cost: spin up a different managed Postgres, run migrations, restore from `pg_dump`. Application code untouched.

## Implementation notes

The walking-skeleton pre-slice in the approved meta-plan (`C:\Users\Alessandro\.claude\plans\hey-so-i-want-snappy-wirth.md`) executes the adoption:

1. `uv add sqlalchemy[asyncio]>=2.0 asyncpg>=0.29 alembic>=1.13`; commit `pyproject.toml` + lockfile.
2. `docker-compose.yml` at repo root with a Postgres 16 service + named volume for dev data.
3. `uv run alembic init -t async services/api/migrations` to scaffold the async migration env.
4. `services/api/src/altune/platform/db.py` — exports `engine` and `async_sessionmaker(engine, expire_on_commit=False)`.
5. Wire into `services/api/src/altune/platform/app.py` via FastAPI `lifespan` for engine disposal.
6. Extend `services/api/src/altune/platform/config.py` with `DATABASE_URL` (ADR-0004 will add `HARDCODED_USER_ID` and `ENV`).
7. First Alembic revision is a no-op marker (`pass` in `upgrade`/`downgrade`) confirming the toolchain runs.
8. `/health` runs `SELECT 1` and returns `{"status":"ok","db":"ok"|"down"}`.
9. `tests/integration/test_health_db.py` uses testcontainers Postgres to assert the health endpoint with a real DB.

Detailed slicing happens during `/feature-plan` for any feature that touches persistence. For the walking-skeleton pre-slice, the meta-plan is the authoritative sequence.

## Vault references

- [vault: wiki/concepts/Hexagonal Architecture.md] — outbound (driven) adapters implement persistence ports defined by the application core.
- [vault: wiki/concepts/Repository Pattern.md] — one repository per Aggregate Root; concrete implementation is the SQLAlchemy class.
- [vault: wiki/concepts/Integration Testing.md] — testcontainers Postgres is the recommended approach for adapter-level DB integration tests.
- [vault: wiki/sources/Domain-Driven Hexagon - Sairyss.md] — practical reference for DDD-aware repository design.

## Related

- Brainstorm: `docs/brainstorms/2026-05-26-persistence-stack.md`
- Predecessor: `docs/adr/0002-stack-expo-fastapi.md` — explicitly deferred this decision in its Deferred Decisions section.
- Companion (forthcoming): `docs/adr/0004-multi-tenancy-posture.md` — adds `HARDCODED_USER_ID`, `ENV`, the `user_id NOT NULL` rule, and the prod-startup guard. Together with this ADR, 0003 + 0004 unlock the walking-skeleton pre-slice.
- Rule files this ADR makes load-bearing: `.claude/rules/adapters-layer.md`, `.claude/rules/migrations.md`, `.claude/rules/python-backend.md`.
