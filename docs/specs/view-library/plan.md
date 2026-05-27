# view-library — implementation plan

Spec: [docs/specs/view-library/spec.md](spec.md)
Status: Draft (awaiting plan-reviewer subagent).

## Slices

Ten vertical slices, ordered for shippability. Each is 2–5 minutes of implementation. TDD discipline per slice: failing test → minimum impl → green → optional refactor. Each layer's test runs in its declared pytest marker (`unit` / `integration` / `e2e`).

### Slice 1: Track domain model + glossary entry

- **Acceptance criterion**: foundation for AC#1, AC#4
- **Goal**: `Track` aggregate + `TrackId` value object exist; `Track` is the canonical name in [docs/ubiquitous-language.md](../../ubiquitous-language.md), `Song` is banned.
- **Files**:
  - `services/api/src/altune/domain/catalog/__init__.py` (new, empty)
  - `services/api/src/altune/domain/catalog/track.py` (new — frozen `@dataclass` with `id: TrackId, user_id: UserId, title: str, artist: str, album: str | None, duration_seconds: int | None, added_at: datetime`; `__post_init__` rejects empty title/artist and negative `duration_seconds`)
  - `services/api/src/altune/domain/catalog/track_id.py` (new — frozen wrapper around UUID, mirrors `domain/shared/user_id.py` pattern)
  - `services/api/tests/unit/altune/domain/catalog/test_track.py` (new)
  - `docs/ubiquitous-language.md` (edit — add `Track` glossary entry; mark `Song` as banned legacy term)
- **Failing test first** (RED commit covers the first; others land in the same slice as same-test-file follow-ons): `test_track_rejects_empty_title`. Same-slice follow-ons: `test_track_rejects_empty_artist`, `test_track_rejects_negative_duration_seconds`, `test_track_is_frozen`, `test_track_equality_by_value`.
- **Verify**: `uv run pytest tests/unit/altune/domain/catalog/test_track.py -v`

### Slice 2: `TrackRepository` port + `InMemoryTrackRepository` test double

- **Acceptance criterion**: AC#2, AC#3, AC#8 (enables unit-testable use case)
- **Goal**: abstract `TrackRepository` port in `application/catalog/`; an in-memory fake under `tests/_doubles/` for use-case unit tests (no DB).
- **Files**:
  - `services/api/src/altune/application/catalog/__init__.py` (new, empty)
  - `services/api/src/altune/application/catalog/ports.py` (new — `TrackRepository` Protocol with `async def list_for_user(user_id, limit, offset) -> tuple[Sequence[Track], int]`)
  - `services/api/tests/_doubles/__init__.py` (new)
  - `services/api/tests/_doubles/in_memory_track_repository.py` (new — implements the protocol, sorts by `(added_at desc, id desc)`, filters by `user_id`)
  - `services/api/tests/unit/altune/application/catalog/test_in_memory_track_repository.py` (new)
- **Failing test first**: `test_in_memory_repo_lists_only_current_user_tracks_in_correct_order`
- **Verify**: `uv run pytest tests/unit/altune/application/catalog/test_in_memory_track_repository.py -v`

### Slice 3: `ListTracks` use case

- **Acceptance criterion**: AC#1 (order), AC#2 (`has_more` computation), AC#3 (terminal condition is downstream), AC#4 (user scoping at port level)
- **Goal**: pure-Python use case that takes `(user_id: UserId, limit: int, offset: int)` and returns a typed result with `items, total, limit, offset, has_more`. Pure function over the repository port; no SQL, no FastAPI.
- **Files**:
  - `services/api/src/altune/application/catalog/list_tracks.py` (new — dataclass `ListTracksInput`, dataclass `ListTracksOutput`, `ListTracks` class with `execute()`; emits `tracks_listed` structlog event)
  - `services/api/tests/unit/altune/application/catalog/test_list_tracks.py` (new)
- **Failing test first**: `test_list_tracks_has_more_true_when_more_rows_exist`
- **Verify**: `uv run pytest tests/unit/altune/application/catalog/test_list_tracks.py -v`

### Slice 4: Alembic migration — `create_tracks_table`

- **Acceptance criterion**: AC#1, AC#4 (schema must exist for integration tests)
- **Goal**: second Alembic revision (revises from `08b831424865`) creates `tracks` per spec schema. Reversibility verified by `alembic downgrade base && alembic upgrade head`.
- **Files**:
  - `services/api/migrations/versions/<rev>_create_tracks_table.py` (new — generated via `uv run alembic revision -m "create tracks table"`, then hand-edited to add the schema)
- **Failing test first**: none required — migration verified by alembic invocation (covered in verify step).
- **Verify** (idempotent — handles pre-existing dev state by going to `base` first; explicit `echo OK` confirms exit 0): `docker compose up -d postgres && DATABASE_URL=postgresql+asyncpg://altune:altune_dev@localhost:5432/altune uv run alembic downgrade base && uv run alembic upgrade head && uv run alembic downgrade base && uv run alembic upgrade head && echo OK`

### Slice 5: `SqlAlchemyTrackRepository` outbound adapter

- **Acceptance criterion**: AC#1, AC#2, AC#4
- **Goal**: real Postgres-backed implementation of the port. SQLAlchemy 2.0 async `mapped_column`-style model `TrackRow`; `to_domain()` / `from_domain()` conversions; query uses `ORDER BY added_at DESC, id DESC LIMIT $1 OFFSET $2` plus a `SELECT COUNT(*)` in the same session for `total`.
- **Files**:
  - `services/api/src/altune/adapters/outbound/persistence/catalog/__init__.py` (new)
  - `services/api/src/altune/adapters/outbound/persistence/catalog/track_row.py` (new — SQLAlchemy declarative model + `Base` if not already defined elsewhere)
  - `services/api/src/altune/adapters/outbound/persistence/catalog/track_repository.py` (new — implements the port, wraps `AsyncSession`)
  - `services/api/tests/integration/test_sqlalchemy_track_repository.py` (new — uses `testcontainers` Postgres; seeds two users with explicit literal UUIDs)
- **Failing test first**: `test_sqlalchemy_track_repo_returns_only_current_user_rows_in_order`
- **Verify**: `uv run pytest -m integration tests/integration/test_sqlalchemy_track_repository.py -v` (needs Docker)

### Slice 5b: Shared port contract test — `InMemoryTrackRepository` vs `SqlAlchemyTrackRepository`

- **Acceptance criterion**: AC#1, AC#2, AC#4 — guards against the InMemory/real-DB drift risk from the Risks section
- **Goal**: a single parametrized pytest module that runs the same scenarios against both repository implementations. Both must agree on: ordering, user isolation, `total` correctness, `has_more` computation, empty case. Lives under `tests/contract/` because it's neither pure unit nor pure integration — it's a port-contract conformance test.
- **Files** (in this order — `pyproject.toml` first, because `pyproject.toml` has `--strict-markers` and the test file using `@pytest.mark.contract` would be rejected without registration):
  - `services/api/pyproject.toml` (edit — add `"contract: shared port-contract tests parametrized over multiple adapter implementations",` to `[tool.pytest.ini_options] markers`)
  - `services/api/tests/contract/__init__.py` (new)
  - `services/api/tests/contract/test_track_repository_contract.py` (new — `@pytest.mark.parametrize("repo_factory", [_in_memory_factory, _sqlalchemy_factory])` over a `TrackRepository`-returning fixture; tests share scenarios)
- **Failing test first**: `test_both_repositories_return_same_ordering_for_same_seed`
- **Verify**: `uv run pytest -m contract tests/contract/test_track_repository_contract.py -v` (the SQLAlchemy parametrization needs Docker; the InMemory one doesn't — both run under one marker for honest pass-rate)

### Slice 6: HTTP inbound adapter — `GET /v1/tracks`

- **Acceptance criterion**: AC#1, AC#2, AC#7
- **Goal**: FastAPI router under `adapters/inbound/http/catalog/`, mounted in `platform/app.py`. Pydantic `TrackResponse` and `ListTracksResponse` DTOs. Query params `limit: int = 50, ge=1, le=200` and `offset: int = 0, ge=0`. Dependency injection wires `ListTracks` with a session from `app.state.sessionmaker`. Emits `http_get_tracks_request` structlog event.
- **Files**:
  - `services/api/src/altune/adapters/inbound/http/catalog/__init__.py` (new)
  - `services/api/src/altune/adapters/inbound/http/catalog/dto.py` (new — Pydantic request/response models)
  - `services/api/src/altune/adapters/inbound/http/catalog/router.py` (new — FastAPI APIRouter with `GET /v1/tracks`)
  - `services/api/src/altune/platform/app.py` (edit — mount catalog router; wire `get_session` dependency)
  - `services/api/src/altune/platform/auth.py` (new — `current_user_id` FastAPI dependency reading `settings.hardcoded_user_id`, per ADR-0004)
  - `services/api/tests/e2e/test_tracks_route.py` (new — uses `TestClient` + `testcontainers` + Settings override; covers happy path AND 422 parameterized over `{limit=0, limit=201, offset=-1}`)
- **Failing test first** (RED on the first; follow-ons land in the same slice file): `test_get_tracks_returns_paginated_response_for_current_user`. Same-slice follow-ons: `test_get_tracks_422_for_limit_zero`, `test_get_tracks_422_for_limit_over_max`, `test_get_tracks_422_for_negative_offset`, `test_get_tracks_isolates_users` (two-user Settings override seeds both, asserts user A's response contains zero of user B's rows — this is the e2e end of AC#4's coverage, complementing slice 5's SQL-level test).
- **Verify**: `uv run pytest -m e2e tests/e2e/test_tracks_route.py -v` (needs Docker)

### Slice 7 (manual): Dev-seed SQL for manual demo

- **Acceptance criterion**: supports manual verification of the full stack (not gated by automation)
- **Goal**: a hand-written `seed_dev_tracks.sql` with 10–20 rows for the hardcoded dev `user_id`, runnable via `psql`. Dev tooling — verified manually as part of Slice 10's end-to-end smoke, not via CI.
- **Files**:
  - `services/api/scripts/seed_dev_tracks.sql` (new — `INSERT INTO tracks (id, user_id, title, artist, album, duration_seconds, added_at) VALUES ...` for the hardcoded `00000000-0000-0000-0000-000000000001` user id)
  - `services/api/scripts/README.md` (new — one paragraph: "How to seed dev tracks: `docker exec -i altune-postgres-dev psql -U altune -d altune < services/api/scripts/seed_dev_tracks.sql`")
- **Failing test first**: none — this is a dev artifact, not behavior code.
- **Verify**: manual; the actual smoke (`curl /v1/tracks` returning seeded rows) is the Slice 10 manual check.

### Slice 8: Mobile API client — `shared/api-client/tracks`

- **Acceptance criterion**: AC#1 (client-side contract)
- **Goal**: first occupant of `apps/mobile/src/shared/api-client/`. Typed `getTracks(limit, offset)` function returning the `ListTracksResponse` shape (manually mirrored from the backend DTO). Hand-rolled `fetch` wrapper for now; OpenAPI codegen is a future ADR.
- **Files**:
  - `apps/mobile/src/shared/api-client/index.ts` (new — `apiBase` config, base `fetch` wrapper)
  - `apps/mobile/src/shared/api-client/types.ts` (new — TypeScript mirrors of `TrackResponse`, `ListTracksResponse`)
  - `apps/mobile/src/shared/api-client/tracks.ts` (new — `getTracks` function)
  - `apps/mobile/src/shared/api-client/__tests__/tracks.test.ts` (new — Jest test using `fetch` mock)
- **Failing test first**: `getTracks_returns_typed_paginated_response_on_200`
- **Verify**: `cd apps/mobile && npm test -- shared/api-client`

### Slice 9: `useLibrary` hook with `useInfiniteQuery`

- **Acceptance criterion**: AC#3 (terminal condition — stops on `has_more=false`)
- **Goal**: React Query hook wrapping `getTracks`. Returns the merged-page items array, current `total`, `isLoading`, `error`, `fetchNextPage`. Stops requesting when `has_more=false`.
- **Files**:
  - `apps/mobile/src/features/library/hooks/useLibrary.ts` (new — `useInfiniteQuery` with `getNextPageParam` returning `undefined` when `has_more=false`)
  - `apps/mobile/src/features/library/__tests__/useLibrary.test.ts` (new — uses `@testing-library/react-hooks` + Query Client wrapper; mocks `getTracks`)
- **Failing test first**: `useLibrary_does_not_fetch_next_page_when_has_more_false`
- **Verify**: `cd apps/mobile && npm test -- features/library/hooks`

### Slice 10: `LibraryScreen` with happy / empty / error states

- **Acceptance criterion**: AC#1 (renders), AC#5 (empty state), AC#6 (error state + retry)
- **Goal**: the screen the user opens. `FlatList` over `useLibrary()` items; renders the three states. Mounted at `/library` via Expo Router.
- **Files**:
  - `apps/mobile/src/features/library/ui/LibraryScreen.tsx` (new — switches on `isLoading | error | empty | items`; renders rows with `id` keyExtractor)
  - `apps/mobile/src/features/library/ui/LibraryRow.tsx` (new — single track row showing title + artist)
  - `apps/mobile/src/app/library.tsx` (new — Expo Router page that re-exports `LibraryScreen`)
  - `apps/mobile/src/features/library/__tests__/LibraryScreen.test.tsx` (new — uses `@testing-library/react-native`; covers empty + error + retry click)
- **Failing test first**: `LibraryScreen_renders_empty_testid_when_no_tracks`
- **Verify**: `cd apps/mobile && npm test -- features/library/ui` and **manual**: `docker compose up -d postgres && DATABASE_URL=... uv run uvicorn altune.platform.app:app & cd apps/mobile && npx expo start` — open the app, see seeded tracks.

## Risks

Lifted from the spec's Risks section + vault anti-patterns:

- **Schema bloat creep.** The implementer (me) will be tempted to add `spotify_id`, `isrc`, etc. while writing slice 4 because they're "obviously useful." The plan-reviewer should reject any column in slice 4 that isn't required by AC#1–AC#8. (Spec risk #1.)
- **`COUNT(*)` cost ignored.** Slice 5 issues a `COUNT(*)` per page request. At v1 scale this is fine; at 100k+ rows it's a problem. The spec accepts this (per-request snapshot semantics); the plan must not silently switch to a cached count or a window-function approach without surfacing the change. (Spec Design Considerations.)
- **`HARDCODED_USER_ID` plumbing.** Slice 6 introduces `platform/auth.py` with `current_user_id`. The function must read `settings.hardcoded_user_id`, NOT a hardcoded literal in code. Slice 6's e2e test must assert that two test users (via Settings override) get isolated results — covered by AC#4 but easy to skip.
- **`InMemoryTrackRepository` drifts from `SqlAlchemyTrackRepository`.** Both implement the same port but their unit/integration test suites grade them separately. If one's behavior diverges (e.g., InMemory sorts ascending, SQLAlchemy sorts descending), tests pass but production breaks. Mitigation: **Slice 5b** runs the same scenarios against both implementations via parametrized fixtures.
- **Mobile bundling pitfalls.** `@tanstack/react-query` is new. Slice 9's test must use a `QueryClientProvider` wrapper; forgetting it causes "No QueryClient set" runtime errors. (Vault anti-pattern: testing async UI without proper provider context.)
- **Terminology drift.** Slice 1 adds `Track` to the glossary. Slices 5/8 must use `Track` / `TrackRow` / `TrackResponse` consistently. The `terminology-drift` Stop hook will catch most leaks; the plan-reviewer should grep diffs for `Song`.

Vault citations applied:
- `[vault: wiki/concepts/Aggregate.md]` — Track aggregate boundary + invariant enforcement (slice 1)
- `[vault: wiki/concepts/Vertical Slice Architecture.md]` — mobile feature folder discipline (slices 8–10)
- `[vault: wiki/topics/API Design Overview.md]`, `[vault: wiki/concepts/Repository Pattern.md]`, `[vault: wiki/concepts/Hexagonal Architecture.md]` — already cited in the spec

## ADR candidates

- **ADR-0005: `@tanstack/react-query` as the mobile server-state library.** ADR-0002 said "no global state library without ADR." React Query is *server* state, which ADR-0002 implicitly allowed — but adopting it sets the convention for every future mobile feature (no parallel `useState`+`useEffect` cargo cult). The ADR is ~5 minutes — its purpose is to commit the convention, not to deliberate the library choice (which is already implied). **Write ADR-0005 immediately before slice 8 starts.**

No other ADR candidates — the rest of the stack is already covered by ADRs 0001–0004.

## AC coverage map

| AC | Slices |
|----|--------|
| AC#1 (deterministic order) | 1 (model), 3 (use case), 4 (schema + index), 5 (SQL), 5b (cross-impl), 6 (route), 10 (renders) |
| AC#2 (server `has_more`) | 3, 5, 5b, 6 |
| AC#3 (client terminal condition) | 3 (use case computes), 9 (hook stops) |
| AC#4 (multi-tenancy at SQL) | 1 (UserId), 2 (port signature), 5 (WHERE clause), 5b (cross-impl), 6 (e2e two-user test) |
| AC#5 (empty state) | 10 |
| AC#6 (error state + retry) | 10 |
| AC#7 (422 on out-of-range) | 6 (parameterized over three inputs) |
| AC#8 (hexagonal seam discipline) | 2 (unit), 3 (unit), 5 (integration), 5b (contract), 6 (e2e) |

## Commits

Per the project's TDD workflow (`docs/workflows/new-feature.md` step 4), each slice is **two commits**:
- `test(view-library): add failing test for <behavior>` — RED
- `feat(view-library): <summary>` — GREEN
- `refactor(view-library): <improvement>` — only if meaningful

If the cycle is tight enough that splitting feels ceremonial, bundling into one `feat(view-library): <summary>` commit per slice is also acceptable (workflow allows this). 10 slices × ~1.5 commits ≈ 15 commits to ship `view-library`.
