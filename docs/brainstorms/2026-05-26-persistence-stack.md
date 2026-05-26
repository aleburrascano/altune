---
date: 2026-05-26
status: draft
graduates-to: docs/adr/0003-persistence-stack.md
related:
  - docs/adr/0002-stack-expo-fastapi.md
  - .claude/rules/migrations.md
  - .claude/rules/adapters-layer.md
  - "[vault: wiki/concepts/Repository Pattern.md]"
  - "[vault: wiki/concepts/Integration Testing.md]"
---

# Brainstorm — persistence stack for altune backend

## 1. Frame

Altune needs to land persistence before its first feature spec. The Phase 0 walking-skeleton requires a chosen DB driver, an ORM (or explicit absence of one), a migration tool, and dev/prod hosting decisions.

**Constraints (from `CLAUDE.md` + `docs/architecture.md` + ADR-0002):**
- Solo developer; maintenance burden weighs heavily.
- Hexagonal architecture — persistence code lives only in `adapters/outbound/persistence/`; the domain must not know SQL.
- Python 3.12+, FastAPI, async-first I/O (per `services/api/CLAUDE.md`: "DB drivers: async (`asyncpg`, `aiosqlite`)").
- `mypy --strict` is enforced.
- A new Supabase project will host prod (decided alongside this brainstorm) so the old `music-manager` Supabase stays untouched.
- The old DB has real songs data the user is still using — porting happens in its own future spec (`migrate-songs-v1`), but the new stack must be able to consume a Postgres dump.

**What this brainstorm decides:**
- ORM vs raw driver vs vendor SDK.
- Migration tool.
- Dev hosting (where tests + local dev hit a real DB).

**What this brainstorm does NOT decide:**
- Auth (ADR-0004 territory).
- File / blob storage (separate spec when ingest lands).
- Specific table schemas (decided per feature spec).

## 2. Options

Four realistic candidates. Each is paired with the migration tool it implies.

### Option A — **SQLAlchemy 2.0 async + asyncpg + Alembic**
Mature Python ORM (Data Mapper flavor) with first-class async support via the asyncpg driver. Canonical setup is `create_async_engine("postgresql+asyncpg://...")` + `async_sessionmaker(engine, expire_on_commit=False)` [VERIFIED:WebFetch@https://docs.sqlalchemy.org/en/20/orm/extensions/asyncio.html] "create a reusable factory for new AsyncSession instances". Alembic ships an async `env.py` cookbook recipe [VERIFIED:WebFetch@https://alembic.sqlalchemy.org/en/latest/cookbook.html] "Alembic can be used with asyncio applications by leveraging SQLAlchemy's experimental asyncio support".
- License: MIT. Active development. Vault-anchored as the exemplar in [.claude/rules/adapters-layer.md:50-62](.claude/rules/adapters-layer.md#L50-L62) (`SqlAlchemyTrackRepository` is the named example).
- Migration tool: **Alembic** (named in [.claude/rules/migrations.md:19](.claude/rules/migrations.md#L19)).

### Option B — **asyncpg direct (no ORM) + yoyo-migrations**
Just the Postgres driver. Repository adapter writes parameterized SQL by hand and constructs domain objects directly. Migrations are raw `.sql` files managed by yoyo-migrations (or hand-rolled via a small script).
- License: Apache 2.0 (asyncpg). Active.
- No ORM means no Row class, no session, no unit-of-work — we re-implement the slices we want.

### Option C — **supabase-py (PostgREST HTTP client)**
The official Supabase Python SDK. Queries go over HTTP to PostgREST: `client.from_("tracks").select("*").execute()` [VERIFIED:WebFetch@https://github.com/supabase/supabase-py/blob/main/src/postgrest/README.md] "async with AsyncPostgrestClient". Not a direct DB connection — a vendor REST layer.
- License: MIT. Active.
- Migrations: Supabase CLI's `supabase migration` (separate ecosystem from Alembic; tied to Supabase platform).

### Option D — **Tortoise ORM + Aerich**
Django-style async ORM with active-record-flavored models. Aerich is its sibling migration tool.
- License: Apache 2.0. Smaller community than SQLAlchemy. Vault has no notes citing it; Sairyss' DDD+Hexagonal reference uses SQLAlchemy-equivalent patterns in TypeScript.

## 3. Decision criteria

Weights tuned for solo + production-grade rebuild (the user's stated quality bar). Weight 3 is non-negotiable; weight 1 is nice-to-have.

| Criterion | Weight | Why this weight |
|-----------|--------|-----------------|
| Hexagonal / DDD fit | 3 | Project constitution. Repository pattern lives or dies on this. [vault: wiki/concepts/Repository Pattern.md] |
| Maintenance burden | 3 | Solo dev; the previous project died from maintenance debt. |
| Documentation quality | 3 | Claude has to find current docs reliably. Context7 was checked for each option. |
| Stack integration | 2 | Rules + CLAUDE.md already anticipate a specific shape. |
| Lock-in risk | 2 | The new DB host is Supabase — but that's deployment, not code. The application layer must stay vendor-neutral. |
| Cost | 2 | All Postgres-compatible hosting is cheap at our scale. Roughly equal. |
| Performance | 1 | Library-scale data (thousands of tracks, not millions). Not the bottleneck. |

## 4. Trade-off matrix

Scores 1 (worst) → 5 (best). Numerical totals = sum(score × weight). Totals are a starting point, not the verdict.

| Criterion (weight)             | A: SQLAlchemy 2.0 + asyncpg + Alembic | B: asyncpg direct + yoyo | C: supabase-py + Supabase CLI | D: Tortoise + Aerich |
|--------------------------------|---------------------------------------|--------------------------|-------------------------------|----------------------|
| Hexagonal / DDD fit (3)        | 5 — Data Mapper is in the rule example | 5 — explicit row→domain mapping | 2 — PostgREST shape leaks into repo | 3 — active-record fights aggregate rules |
| Maintenance burden (3)         | 4 — boilerplate, but well-trodden     | 2 — every CRUD is hand-written | 4 — terse API, but vendor-bound | 3 — smaller ecosystem; surprise edges |
| Documentation quality (3)      | 5 — vast, current, Context7-indexed   | 4 — asyncpg docs are good; migrations less so | 4 — official docs solid, narrower scope | 3 — adequate but thin |
| Stack integration (2)          | 5 — already named in adapters-layer.md and migrations.md | 3 — would require carving out our own migration story | 2 — would require ADRs for migration tool + RPC pattern | 3 — works but cross-grain with the codified examples |
| Lock-in risk (2)               | 4 — Postgres-portable; ORM is removable | 5 — driver-only; near-zero lock-in | 1 — couples whole repo layer to Supabase's HTTP API | 4 — Postgres-portable |
| Cost (2)                       | 4                                     | 4                        | 4                             | 4                    |
| Performance (1)                | 4 — small ORM overhead                 | 5 — driver only          | 3 — HTTP hop per query        | 4                    |
| **Weighted total**             | **76**                                | **64**                   | **52**                        | **60**               |

## 5. Recommendation

**Adopt Option A — SQLAlchemy 2.0 async + asyncpg + Alembic.**

The vault's Repository Pattern note pairs naturally with SQLAlchemy's Data Mapper [vault: wiki/concepts/Repository Pattern.md], the project's rules already cite this exact stack as the canonical example [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\.claude\rules\adapters-layer.md#L50-L62], and Alembic is pre-named by the migrations rule [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\.claude\rules\migrations.md#L19]. Adopting A means the first feature's adapter matches the rule example one-to-one and the rest of the codebase is built on a foundation Claude can reliably pattern-match against in future sessions.

**Named trade-off you must accept:**
> SQLAlchemy adds more ceremony than supabase-py for a five-row tracks table. You write `TrackRow ↔ Track` mapping in the repository adapter by hand, and you maintain Alembic migrations by hand. In exchange, the domain layer stays free of any vendor, the integration test suite can stand up a real Postgres via testcontainers without touching Supabase, and if Supabase becomes painful you swap the connection string and nothing else changes. The old `music-manager`'s Supabase coupling is exactly what made it brittle — this is the deliberate fix.

**Why not the others (briefly):**
- **B (asyncpg direct)**: the migration tool gap is real. yoyo-migrations is fine but not what the project's rule file expects, and you'd reinvent unit-of-work, autoflush boundaries, and bulk-insert ergonomics. Reach for B only if SQLAlchemy ever proves too heavy.
- **C (supabase-py)**: PostgREST is great for client-side JS apps where you've already accepted the vendor. For a Python backend that already has its own HTTP surface, routing queries via another HTTP hop is two networks where there should be one, and the "swap adapter" promise of the hexagon evaporates because the entire repository code becomes Supabase verbs.
- **D (Tortoise)**: smaller community and a migration tool (Aerich) less mature than Alembic. The vault doesn't reference it; no rule file anticipates its shape; you'd be the test case.

## 6. Hosting decision (paired with A)

- **Dev:** local Postgres 16 in Docker via `docker-compose.yml` at repo root. testcontainers-python spins ephemeral instances for integration tests (per [vault: wiki/concepts/Integration Testing.md] "Spin up a real database (Postgres, MySQL) in Docker using tools like Testcontainers").
- **Prod:** new Supabase project (separate from the old `music-manager` project). `DATABASE_URL` points at it; the application code never imports `supabase-py` — only the connection string differs from dev. Auth and storage features of Supabase are NOT used in v1; they get their own ADRs if/when adopted.

## 7. Concrete next steps if approved

1. Hand off to `/adr-write` → produces `docs/adr/0003-persistence-stack.md` capturing this decision + rationale.
2. `uv add sqlalchemy[asyncio]>=2.0 asyncpg>=0.29 alembic>=1.13` and commit pyproject.toml + lockfile.
3. Add `docker-compose.yml` at repo root with a Postgres 16 service.
4. Initialize Alembic with the async template: `uv run alembic init -t async migrations`.
5. Create `services/api/src/altune/platform/db.py` exposing engine + `async_sessionmaker(engine, expire_on_commit=False)`; wire into `platform/app.py` via FastAPI lifespan.
6. Extend `platform/config.py` with `DATABASE_URL` (+ ADR-0004's `HARDCODED_USER_ID`, `ENV` once that ADR lands).
7. First Alembic migration is a no-op marker (`pass` in `upgrade`/`downgrade`) — just confirms the toolchain runs end-to-end.
8. Update `/health` to attempt `SELECT 1` and report `{"status":"ok","db":"ok"|"down"}`.
9. Integration test under `tests/integration/test_health_db.py` using testcontainers Postgres.

That sequence IS Phase 0 step 1 + the walking skeleton from the approved plan.
