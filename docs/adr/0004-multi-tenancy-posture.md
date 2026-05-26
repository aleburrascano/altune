# ADR-0004: Multi-tenancy posture — hardcoded user id for dev, NOT NULL user_id column from day 1, prod-startup guard, no auth middleware yet

- **Status:** Accepted
- **Date:** 2026-05-26
- **Deciders:** solo + Claude
- **Context tags:** [policy, security, layer]

## Context

ADR-0003 just landed the persistence stack. The first persisted table (`tracks`) is imminent, and every persisted row in altune is in principle tenant-scoped (a track belongs to a user's library). Two forces converge:

1. **The prior project's failure mode.** The legacy `music-manager` Supabase `songs` table has `user_id UUID` but the Python repositories were authored with a default (`def get_songs(user_id=None)`), so multi-tenancy in code was weaker than in the schema. Tests passed without `user_id`; production rows leaked across what should have been tenant boundaries. The architecture audit flagged this as Critical #1.
2. **No auth in v1.** Altune is built solo for personal use first. Adding real authentication now (Supabase OAuth, FastAPI-Users, or an external IdP) is scope creep and warrants its own ADR.

If we wait to "do multi-tenancy later when auth lands," we will pay it twice: once now for every table without `user_id`, again to add and backfill the column. The old project paid both. We can avoid that by **locking the column shape now and providing a temporary stand-in for the identity source**.

The vault has no concept note for "auth deferral" — this ADR is project-specific risk mitigation, anchored to the Observability note's principle that cross-cutting context (user_id, tenant_id) is a first-class concern at every layer.

## Decision

Adopt four interlocking rules for v1:

1. **`user_id UUID NOT NULL` on every tenant-scoped table from day 1.** No defaults, no nullable column. Alembic migrations adding tenant-scoped tables must declare the column at creation; the `migrations.md` rule already requires reversibility and explicit intent for any schema change.
2. **A single `current_user_id` FastAPI dependency** is the only authoritative source of "who is the caller" inside the application layer. In v1 it returns `settings.HARDCODED_USER_ID` parsed from env. Every use case takes `user_id: UserId` as input; no use case reads env directly.
3. **Startup guard.** `services/api/src/altune/platform/app.py` refuses to start if `ENV == "production"` and `HARDCODED_USER_ID` is set. Refusal is a fail-fast: log the violation and exit non-zero. One integration test asserts this.
4. **No auth middleware yet.** No JWT verification, no session cookies, no user-table foreign key. The future auth ADR will: (a) swap the `current_user_id` dependency to read from a verified token, (b) optionally introduce a `users` table and FK from `tracks.user_id` to `users.id`, (c) leave every other layer untouched.

## Alternatives considered

| Alternative | Why not |
|---|---|
| **Defer the `user_id` column until auth lands** | This is what the old project did. Adding the column later requires a backfill migration on real data plus a deploy where the API briefly accepts rows without `user_id`. Pay it now, pay it once. |
| **`user_id UUID DEFAULT gen_random_uuid()`** | Exactly the old project's bug. The default papers over "we forgot to pass user_id" in code and silently fragments tenancy across random UUIDs. Forbidden. |
| **Adopt Supabase Auth (or any real auth) now** | Increases scope of the first features (login screen, JWT verification middleware, session refresh) and locks in an auth vendor before we have any reason to. Real auth gets its own ADR when the first feature actually needs it. |
| **Hardcode the user id in Python code, not env** | Makes it impossible to override per environment (dev vs CI vs personal phone build); a single hardcoded constant in source means a code change to swap users in dev. Env-var indirection costs nothing and avoids the trap. |
| **No startup guard; trust the operator** | The exact failure mode this ADR exists to prevent. A `HARDCODED_USER_ID` env leaking into a prod deploy is silent and indistinguishable from real traffic until rows go to the wrong tenant. Cheap guard, catastrophic prevention. |

## Consequences

### What becomes easier
- **Future auth introduction is a swap.** Replacing the `current_user_id` dependency with one that reads a verified JWT subject means every use case keeps its signature; no column shape changes; no backfill.
- **Tenant isolation is testable from day 1.** Integration tests seed two distinct `user_id`s into `tracks` and assert that a request for user A returns zero of user B's rows. The test fails if the `WHERE user_id` clause is stripped from any repository.
- **Logs carry tenant context immediately.** Every structured log record under user-driven code paths includes `user_id` because the use case has it. This matches the Observability note's "cross-cutting context (user_id, tenant_id)" recommendation.
- **The old project's #1 architectural risk is structurally prevented**, not "promised by convention."

### What becomes harder
- **Dev setup requires `HARDCODED_USER_ID` in `.env`** (or env shell). `.env.example` documents it; forgetting it surfaces as a startup error (intentional). Mild friction in exchange for the prod-startup guard's protection.
- **Every use case signature carries `user_id: UserId`** as a first-class input. This is more verbose than implicit context, but reading a use case's signature now answers "is this tenant-scoped?" without inspecting the body. The verbosity is the documentation.
- **CI integration tests need `HARDCODED_USER_ID` set** to boot the app. The walking-skeleton pre-slice (ADR-0003 implementation notes) handles this in conftest fixtures.

### What we're committing to (and the cost to reverse)
- **`user_id UUID NOT NULL` on every tenant-scoped table.** Reversal would mean migrating every table to make `user_id` nullable — moderate-cost migration, but inverse to the failure mode we care about, so the cost of reversing is exactly what we want it to be (high).
- **`current_user_id` as the single identity port into the application layer.** When real auth lands, this dependency changes its body but keeps its signature. Reversal: trivial (the indirection IS the seam).
- **Env-var-based dev identity.** Swapping to a different mechanism (e.g., a CLI flag, a config file) is a single-file change in `platform/config.py`.
- **Prod-startup guard.** Removing the guard is a one-line change; we'd only remove it when real auth is in place and the env var is no longer meaningful.

## Implementation notes

Walking-skeleton pre-slice (ADR-0003 implementation notes step 6 + this ADR):

1. Extend `services/api/src/altune/platform/config.py`:
   - `DATABASE_URL: str`
   - `HARDCODED_USER_ID: UUID | None = None`
   - `ENV: Literal["development", "test", "production"] = "development"`
2. Add a constructor-time check in `Settings` (or in `platform/app.py`'s lifespan) that raises `RuntimeError("HARDCODED_USER_ID must not be set when ENV=production")` if both conditions hold.
3. Define `UserId` as a thin newtype/`NewType` value object under `services/api/src/altune/domain/shared/user_id.py` (one of the few cross-context types). It is *not* a full DDD aggregate — just a typed UUID alias so use-case signatures read as `user_id: UserId, not user_id: UUID`.
4. Define `current_user_id` in `services/api/src/altune/platform/auth.py` (single file, single function) returning `UserId(settings.HARDCODED_USER_ID)`. Future auth ADR swaps this file's body.
5. `.env.example` documents `HARDCODED_USER_ID=00000000-0000-0000-0000-000000000001` for dev.
6. Integration test `tests/integration/test_startup_guard.py` asserts the app refuses to construct `Settings()` when `ENV=production` and `HARDCODED_USER_ID` is set.

The walking-skeleton's `/health` endpoint does NOT depend on `current_user_id` — health is unauthenticated by design.

## Vault references

- [vault: wiki/concepts/Observability.md] — cross-cutting context (user_id, tenant_id) is first-class in logs and traces; this ADR enforces that at the schema and use-case level.

(No dedicated multi-tenancy concept note exists in the software-architecture-design vault. This ADR records a project-specific risk-mitigation posture; if a future ADR adopts a richer multi-tenancy strategy — schema-per-tenant, row-level security policies, etc. — it supersedes this one.)

## Related

- Predecessor: `docs/adr/0003-persistence-stack.md` — locks in the schema mechanism this ADR shapes.
- Follow-on (forthcoming, not on the current horizon): `docs/adr/NNNN-auth.md` — when real auth lands, it swaps the `current_user_id` dependency body and optionally adds a `users` table.
- Rule files this ADR makes load-bearing: `.claude/rules/migrations.md` (NOT NULL columns at creation), `.claude/rules/adapters-layer.md` (repositories must accept `user_id` as a parameter, never read env).
