# View library

> Spec for `view-library` — version 1, drafted 2026-05-26.
> Authors: solo + Claude.
> Status: Shipped (all 11 slices + ADR-0005 committed 2026-05-27 — see commits 527747b through 838b95b).

## Problem

The user has a music library that's currently invisible on mobile. The legacy `music-manager` (Flask + React web) shows it; altune (the rebuild) hasn't shown anything yet. Until at least *reading* the library works on the phone, every other feature — add, edit, delete, play — has no surface to attach to. "What do I own?" is the question nothing else can be asked without.

This is the first user-visible feature of altune.

## User value

Open the altune mobile app and see a scrollable list of tracks in the library, ordered most recently added first. Scrolling near the bottom loads the next batch automatically. The screen behaves coherently when there are zero tracks (designed empty state) and when the server is unhealthy (designed error state with retry), not just on the happy path.

## Acceptance criteria

Each one is testable. Each one will become at least one automated test.

1. **AC#1 (read happy path, deterministic order)** — Given N seeded `tracks` rows for the current user (`HARDCODED_USER_ID` per ADR-0004), when the mobile screen mounts and the client calls `GET /v1/tracks?limit=50&offset=0`, then the response is HTTP 200 with body shape `{items, total, limit, offset, has_more}` and the screen's `FlatList` renders the first 50 tracks (title + artist on each row) ordered by `(added_at DESC, id DESC)`. The `id`-as-tiebreaker is what makes the order stable when multiple rows share `added_at` (common from bulk seeds and future imports); without it, page boundaries shift between calls.

2. **AC#2 (server: `has_more` is derived correctly)** — Given any `(limit, offset)` query, the server's response satisfies `has_more == ((offset + len(items)) < total)`. Asserted by the repository integration test across the matrix `(N=0, N=limit, N=limit+1, N=2*limit)`.

3. **AC#3 (client: terminal condition)** — Given a sequence of `GET /v1/tracks` calls driven by `onEndReached`, when a response with `has_more=false` arrives, then the mobile client issues **no** further `GET /v1/tracks` calls for that screen session. Asserted by the component test using a mocked client whose call count is observed.

4. **AC#4 (multi-tenancy enforced at SQL)** — Given two distinct `user_id` values seeded into the `tracks` table with **explicit literal UUIDs** (so the test can assert set equality), when user A calls `GET /v1/tracks`, then the response's `items[].id` set is exactly equal to user A's seeded `id`s and contains **none** of user B's. Asserted by the repository integration test seeding both users explicitly.

5. **AC#5 (designed empty state)** — Given the current user has zero tracks, when the screen finishes its initial load, then the rendered tree contains a node `testID="library-empty"` with non-empty visible text. Asserted by component test.

6. **AC#6 (designed error state with retry)** — Given `GET /v1/tracks` returns HTTP 5xx, when the screen receives the error, then the rendered tree contains `testID="library-error"` with a button `testID="library-retry"` that, when pressed, re-issues the request. Asserted by component test using a mocked client.

7. **AC#7 (validation contract: 422 on out-of-range query)** — Given any of the three out-of-range inputs `{limit=0, limit=201, offset=-1}`, when the server processes the request, then the response status code is HTTP 422. The response body shape is intentionally **not** constrained by this AC — only the status code — so that a future RFC 7807 problem-details migration (mentioned in Design Considerations for 5xx) can normalize the body uniformly without breaking this AC. Asserted by an e2e test parameterized over the three inputs.

8. **AC#8 (hexagonal seam discipline)** — `ListTracks` use case has a unit test against an `InMemoryTrackRepository` (no DB). `SqlAlchemyTrackRepository` has integration tests against a `testcontainers` Postgres (real SQL). `GET /v1/tracks` has an e2e test via `httpx.AsyncClient` against the in-process app. Each layer's test exists and runs in its declared pytest marker (`unit`, `integration`, `e2e`).

## Out of scope

- Editing, deleting, or adding tracks. Each is its own future spec (`edit-track-metadata`, `delete-track`, `add-track-manually`).
- Audio file upload, playback, scrubbing, queue, mini-player.
- Spotify / YouTube / SoundCloud integration, search, or matching.
- Album art rendering. The `album_art_path` column does not exist in the v1 schema; rendering will land with whatever feature first earns its column.
- Search, filter, or any sort other than `added_at DESC`.
- Authentication. Single user via `HARDCODED_USER_ID` per ADR-0004; real auth is a future ADR and spec.
- Track detail screen. A row tap may navigate, but the destination screen is `view-track-detail`'s spec.
- Porting data from the legacy `music-manager` Supabase. That is `migrate-songs-v1`'s spec; for `view-library` we seed dev manually via a SQL script.

## Design considerations

Patterns and trade-offs surfaced by the vault lookup (per `.claude/rules/vault-consultation.md`):

- [vault: wiki/topics/API Design Overview.md] — REST + offset/limit pagination is the recommended shape for "resource-oriented APIs" with diverse clients. Mobile here is one client; the API stays REST.
- [vault: wiki/concepts/API Design Principles.md] — URI versioning (`/v1/tracks`); plural-noun resource; tolerant-reader behavior on the client (ignore unknown fields).
- [vault: wiki/concepts/Repository Pattern.md] — one repository per aggregate root. `TrackRepository` port in `application/catalog/`; `SqlAlchemyTrackRepository` adapter in `adapters/outbound/persistence/catalog/`.
- [vault: wiki/concepts/Hexagonal Architecture.md] — domain has zero framework deps; the use case takes `user_id: UserId` and `limit: int, offset: int`; framework code (FastAPI, SQLAlchemy) is confined to the adapter ring.

High-level approach (not implementation detail — that's the plan):

- This is a **read** path in the **catalog** bounded context. Catalog is new; this spec is its first occupant.
- It **does** require a new aggregate (`Track`) and value object (`TrackId`), a new port (`TrackRepository`), a new use case (`ListTracks`), and one new outbound adapter (`SqlAlchemyTrackRepository`). Schema for `tracks` is created via a new Alembic migration that revises from the existing no-op marker.
- It **does not** introduce a new external dependency in the backend (SQLAlchemy + asyncpg + alembic landed in ADR-0003). It **does** introduce one mobile dependency: `@tanstack/react-query` for server state — first adoption, documented here, mobile-wide thereafter.

Schema (lands as the second Alembic migration, revises from `08b831424865`):

- `tracks` table — `id UUID PK DEFAULT gen_random_uuid()`, `user_id UUID NOT NULL`, `title TEXT NOT NULL`, `artist TEXT NOT NULL`, `album TEXT NULL`, `duration_seconds INTEGER NULL`, `added_at TIMESTAMPTZ NOT NULL DEFAULT now()`.
- Index `tracks_user_added_idx` on `(user_id, added_at DESC, id DESC)` — the trailing `id DESC` is what makes pagination stable across rows that share `added_at`.
- `id` may be application-minted (e.g. `uuid4()`) instead of DB-minted; the default is for hand-written seed SQL convenience and for any code path that doesn't pre-generate one.
- No FK to a `users` table; no users table exists yet. ADR-0004 explicitly allows this and the future auth ADR adds the FK if appropriate.

Response contract for `GET /v1/tracks?limit=&offset=`:

```json
{
  "items": [
    {"id": "uuid", "title": "str", "artist": "str", "album": "str|null",
     "duration_seconds": "int|null", "added_at": "iso8601 with explicit UTC offset"}
  ],
  "total": 123,
  "limit": 50,
  "offset": 0,
  "has_more": true
}
```

Defaults: `limit=50`, `offset=0`. Bounds: `1 ≤ limit ≤ 200`, `offset ≥ 0`. Out-of-range values return `422 Unprocessable Entity` per FastAPI/Pydantic defaults — covered by AC#5.

`added_at` serializes as ISO-8601 with an explicit UTC offset (e.g. `2026-05-26T14:30:00+00:00`), not the naive `Z` form. Mobile parses it with its standard `Date` constructor; ambiguous formats are forbidden.

`items[].id` is non-null, unique across all pages of a single user's library within one client session, and stable across page refetches. The mobile `FlatList` uses it as its `keyExtractor`.

`total` reflects the row count at the moment of each request; pages can shift between calls if writes happen in between. Acceptable for v1 — no write paths exist yet and the dev library is hand-seeded. Computed via a `COUNT(*)` issued alongside the page query in the same transaction; the cost is fine at library scale (≤ low thousands).

Error responses follow RFC 7807 problem-details where it adds value; otherwise FastAPI's default body suffices for `422` / `5xx`. The mobile client treats both as the same generic error state per AC#4b — the "Retry" button re-issues the GET, which is safe/idempotent [vault: wiki/concepts/Idempotency.md] so no idempotency key is needed.

**Not a Backend for Frontend** [vault: wiki/concepts/Backend for Frontend Pattern.md]. The single REST API serves the single mobile client today. Promotion to BFF is deferred until a second client (web, watch, etc.) is introduced and the per-client data shapes diverge enough to make a shared API a bottleneck. Calling this out so future specs don't drift the API toward mobile-only conveniences that would later need extraction.

Mobile shape:

- `apps/mobile/src/features/library/` — first occupant of `src/features/`.
- Screen: `ui/LibraryScreen.tsx` rendering a `FlatList` with `onEndReached` triggering next-page fetch.
- Hook: `hooks/useLibrary.ts` wrapping `useInfiniteQuery` from `@tanstack/react-query`.
- Client: `apps/mobile/src/shared/api-client/` is created as part of this feature; the typed `getTracks(limit, offset)` lives there. First population of `src/shared/`.

## Dependencies

What this feature requires that must already exist or be built first:

- **Bounded contexts**: none (catalog is created by this spec).
- **Other features**: none — this is feature #1.
- **External services**: local Postgres 16 in Docker for dev (`docker-compose.yml`); a Supabase project for prod (separate from the legacy one).
- **Library/framework additions**:
  - Backend: none beyond ADR-0003's stack.
  - Mobile: `@tanstack/react-query` (latest stable) — adopted here, used by every future feature.

## Risks / open questions

- **Risk**: schema bloat. The instinct will be to add `spotify_id`, `isrc`, `album_art_path`, `genre`, `year` because the legacy `songs` table had them. Mitigation: the v1 `tracks` table has only AC-required columns. Every other column earns entry via its own feature spec + Alembic migration. The Plan agent flagged this exact temptation; the spec calls it out so the plan-reviewer subagent has a reference when grading slices.
- **Risk**: pagination contract drift between mobile and API. Mitigation v1: hand-mirrored TypeScript types in `apps/mobile/src/shared/api-client/`. OpenAPI-driven codegen is its own future spec.
- **Risk**: mobile error/empty states shipping as afterthoughts. Mitigation: AC#4 makes them part of the definition of done; the ux-reviewer subagent will block on a blank screen.
- **Risk**: terminology drift — the legacy code says `song`. Mitigation: this spec adds `Track` as the canonical entry in `docs/ubiquitous-language.md` and bans `Song` in altune code. The `terminology-drift` Stop hook flags the rest; the migrate-songs-v1 spec is where the rename across the data port lives.
- **Risk**: `HARDCODED_USER_ID` invisibly leaks to prod via this feature. Mitigation: the prod-startup guard from ADR-0004 prevents the app from booting in that state. AC#3 plus the integration test verify isolation.
- **Risk**: offset/limit pagination degrades at scale — Postgres scans `offset` rows on each page. Not a v1 concern (libraries are low-thousands at most) but the response shape we commit to now is the one mobile depends on, and a future migration to **keyset pagination** (cursor on `(added_at, id) < (cursor)`) would be a breaking change to the response. Revisit when a single user's library exceeds ~5k tracks or `GET /v1/tracks` p95 > 200ms. The mitigation cost is small if planned (a new query parameter `cursor`, deprecating `offset` over time); large if surprised.
- **Open question**: how is the dev environment seeded with tracks for the manual demo (uvicorn + Expo)? Resolved before plan: a small `services/api/scripts/seed_dev_tracks.sql` (10–20 rows of hand-typed tracks) the developer runs once with `psql`. Not a feature; not a spec; just a dev convenience.
- **Open question**: does the mobile screen need to support pull-to-refresh in v1, or only end-reached? Tentatively: end-reached only for v1. Pull-to-refresh is a 5-minute add later if the developer misses it; not blocking.

## Telemetry

What we'd log / measure in production to know this works:

- **Log events** (structlog, JSON in prod per `platform/logging.py`):
  - `http_get_tracks_request` — emitted by the FastAPI route handler before delegating to the use case. Fields: `user_id`, `limit`, `offset`, `request_id`. Required so the inbound side is separately traceable from the use case side; without it a "wrong user_id wired into the request" bug (the most likely failure mode given ADR-0004's hardcoded id) is invisible.
  - `tracks_listed` — emitted by the `ListTracks` use case on success. Fields: `user_id`, `limit`, `offset`, `returned_count`, `total`, `duration_ms`.
  - `tracks_query_failed` — emitted by the repository on `SQLAlchemyError`. Fields: `user_id`, `error_type`, `error_msg`.
- **Metrics**: deferred — no metrics ADR yet. When that ADR ships, the minimum is request count and p50/p95 latency for `GET /v1/tracks`.
- **Alerts**: none in v1; no on-call.

## Related

- `[vault: wiki/topics/API Design Overview.md]` — REST + pagination patterns
- `[vault: wiki/concepts/API Design Principles.md]` — URI versioning, resource naming, error-response shape
- `[vault: wiki/concepts/Repository Pattern.md]` — one repository per aggregate root
- `[vault: wiki/concepts/Hexagonal Architecture.md]` — port in application, adapter in adapters
- `[vault: wiki/concepts/Backend for Frontend Pattern.md]` — explicitly rejected for v1 (single client)
- `[vault: wiki/concepts/Idempotency.md]` — GET is safe + idempotent, retry button needs no idempotency key
- Predecessor ADRs: `docs/adr/0001-monorepo-layout.md`, `docs/adr/0002-stack-expo-fastapi.md`, `docs/adr/0003-persistence-stack.md`, `docs/adr/0004-multi-tenancy-posture.md`
- Predecessor work: walking-skeleton pre-slice (committed across 8 commits 2026-05-26)
- Successor specs (planned, not written): `view-track-detail`, `add-track-manually`, `migrate-songs-v1`, `edit-track-metadata`, `delete-track`
