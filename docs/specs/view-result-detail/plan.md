# view-result-detail — implementation plan

Spec: docs/specs/view-result-detail/spec.md

Slices are ordered so each is shippable/testable on its own. Backend write-path first
(domain → application → persistence → migration → HTTP), then mobile (client → read-only
screen, independently demoable → Save). Failing-test-first per slice (TDD).

Verify commands: backend from `services/api/` (`pytest <path> -v`); mobile from `apps/mobile/`
(`npx jest <path>`). `mypy --strict` is part of `/verify-end-to-end`, not repeated per slice.

**Dedup-key design (resolves the idempotency mechanism + the B5 wiring gap):** the natural key is a
pure domain normalizer `dedup_key(title, artist, album)` (slice 3). It is **not** a `Track` field,
**not** an argument to `repo.add`, and **not** computed in the use case. Each repository implementation
(in-memory fake, Postgres adapter) computes it itself from the `Track`'s own
`title`/`artist`/`album` + `user_id` by calling the shared normalizer, and the Postgres adapter writes
the result into the `dedup_key` column on INSERT. One normalizer, two callers → identical dedup, no
value threaded through the domain.

**Real repo layout (verified) the slices must match:**
- Migrations: `services/api/migrations/versions/` — Alembic **hash-slug** filenames (e.g.
  `e2bcd72a93f1_*.py`), **not** `0004_`. Current head is `e2bcd72a93f1`; generate with
  `uv run alembic revision -m "..."`, confirm `down_revision` against `uv run alembic heads`, then
  hand-edit (per `.claude/rules/migrations.md`). Migration tests follow
  `tests/integration/test_discovery_migrations.py` (testcontainers Postgres + `pg_catalog` inspection).
- Existing catalog adapter test: `tests/integration/test_sqlalchemy_track_repository.py`; HTTP/route
  coverage: `tests/e2e/test_tracks_route.py` (no `tests/integration/altune/...http/` tree exists).
- In-memory double: `tests/_doubles/in_memory_track_repository.py` (the `add` method lands here, not
  in the test module). `tests/contract/test_track_repository_contract.py` runs both implementations —
  treat it as a regression guard for slices 4 and 7.

**Integration DB:** slices 6–9 run against the **same Postgres testcontainers fixture** the discovery
integration/e2e tests use. No SQLite — `ON CONFLICT`/unique-index behavior is Postgres-specific.

**Ordering hazard (S1):** slices 2–5 are pure domain/application (unit tests only) and stay green
before the migration exists. **Do not** run `test_sqlalchemy_track_repository.py` or any e2e against a
migrated DB until slice 6 (the column-adding migration) lands.

**Glossary placement (terminology-drift hook):** add each glossary term in the slice that introduces
the type — `AcquisitionStatus` in slice 1, `TrackAddedToLibrary` in slice 5 — not deferred to slice 9
(slice 9 only refreshes the existing `Track` entry).

## Slices

### Slice 1: `AcquisitionStatus` value object
- AC: AC#8
- Files:
  - services/api/src/altune/domain/catalog/acquisition_status.py (new)
  - services/api/tests/unit/altune/domain/catalog/test_acquisition_status.py (new)
  - docs/ubiquitous-language.md (edit — add `AcquisitionStatus` term, same commit, to satisfy the terminology-drift hook)
- Domain: `AcquisitionStatus(Enum)` with single member `PENDING = "pending"` (wire = lowercase). Docstring: "saved to library; audio not yet acquired."
- Failing test first: `test_acquisition_status_pending_serializes_to_lowercase`
- Verify: `pytest tests/unit/altune/domain/catalog/test_acquisition_status.py -v`

### Slice 2: Extend `Track` aggregate with `artwork_url` + `acquisition_status`
- AC: AC#8
- Files:
  - services/api/src/altune/domain/catalog/track.py (edit)
  - services/api/tests/unit/altune/domain/catalog/test_track.py (edit)
- Domain: add `artwork_url: str | None` and `acquisition_status: AcquisitionStatus = AcquisitionStatus.PENDING`. **`dedup_key` is NOT added to `Track`** (it's a persistence-computed key, slice 3). Preserve non-empty title/artist + non-negative duration invariants and id-based equality.
- Failing test first: `test_track_defaults_acquisition_status_to_pending`
- Verify: `pytest tests/unit/altune/domain/catalog/test_track.py -v`

### Slice 3: Pure `dedup_key` normalizer (the "same track" rule)
- AC: AC#7
- Files:
  - services/api/src/altune/domain/catalog/dedup.py (new — `def dedup_key(title, artist, album: str | None) -> str`)
  - services/api/tests/unit/altune/domain/catalog/test_dedup.py (new)
- Domain: lower-case, trim, collapse internal whitespace, join `title`/`artist`/`album` with a separator; null album → `""`. Pure, no I/O. Both the fake (slice 4) and the adapter (slice 7) import this.
- Failing test first: `test_dedup_key_is_case_and_whitespace_insensitive_and_handles_null_album`
- Verify: `pytest tests/unit/altune/domain/catalog/test_dedup.py -v`

### Slice 4: `TrackRepository.add` port + in-memory fake (dedup-aware, returns `created`)
- AC: AC#5, AC#7
- Files:
  - services/api/src/altune/application/catalog/ports.py (edit)
  - services/api/tests/_doubles/in_memory_track_repository.py (edit — `add` lands HERE, not in the test module)
  - services/api/tests/unit/altune/application/catalog/test_in_memory_track_repository.py (edit — assertions only)
- Application: add `async def add(self, track: Track) -> tuple[Track, bool]` to the `TrackRepository` Protocol — returns `(persisted_track, created)`; `created=False` = dedup hit returning the existing track. The fake computes the key via `dedup_key(...)` + `track.user_id`.
- Regression guard: `tests/contract/test_track_repository_contract.py` runs both implementations — keep the `InMemoryTrackRepository` constructor/`list_for_user` behavior unchanged so it stays green.
- Failing test first: `test_in_memory_add_returns_existing_and_created_false_on_duplicate` (asserts BOTH the returned track identity AND `created is False` — later slices rely on the bool)
- Verify: `pytest tests/unit/altune/application/catalog/test_in_memory_track_repository.py -v`

### Slice 5: `AddTrackToLibrary` use case + `TrackAddedToLibrary` event
- AC: AC#5, AC#7
- Files:
  - services/api/src/altune/application/catalog/add_track_to_library.py (new)
  - services/api/src/altune/domain/catalog/events.py (new — `TrackAddedToLibrary`, past-tense, `occurred_at`; mirror `domain/discovery/events.py`)
  - services/api/tests/unit/altune/application/catalog/test_add_track_to_library.py (new)
  - docs/ubiquitous-language.md (edit — add `TrackAddedToLibrary` term, same commit)
- Application: build a `Track` from input, call `repo.add`; emit `TrackAddedToLibrary` **only when `created=True`**; on dedup hit return existing track + flag, no event. Does **not** compute the dedup key (repo's job). No ORM.
- Failing test first: `test_add_track_emits_event_on_create_and_skips_on_duplicate`
- Verify: `pytest tests/unit/altune/application/catalog/test_add_track_to_library.py -v`

### Slice 6: Alembic migration — columns + unique dedup index (Postgres)
- AC: AC#7, AC#10
- Files:
  - services/api/migrations/versions/<hash>_add_track_acquisition_and_dedup.py (new — generate via `uv run alembic revision -m "add track acquisition and dedup"`; set `down_revision` to the current head `e2bcd72a93f1`, confirm with `uv run alembic heads`)
  - services/api/tests/integration/test_catalog_migration_dedup.py (new — follow `tests/integration/test_discovery_migrations.py`: testcontainers Postgres, `pg_catalog` column/index inspection)
- Adapter: add `artwork_url TEXT NULL`, `acquisition_status TEXT NOT NULL DEFAULT 'pending'`, `dedup_key TEXT NOT NULL` to `tracks`; `CREATE UNIQUE INDEX uq_tracks_user_dedup ON tracks(user_id, dedup_key)`. Pre-launch: empty table, no backfill.
- Failing test first: `test_migration_adds_track_acquisition_columns_and_unique_dedup_index`
- Verify: `pytest tests/integration/test_catalog_migration_dedup.py -v`

### Slice 7: Postgres `TrackRepository.add` — `ON CONFLICT DO NOTHING → SELECT`
- AC: AC#5, AC#7
- Files:
  - services/api/src/altune/adapters/outbound/persistence/catalog/track_repository.py (edit — implement `add`)
  - services/api/src/altune/adapters/outbound/persistence/catalog/track_row.py (edit — new columns + `dedup_key`)
  - services/api/tests/integration/test_track_repository_add.py (new — testcontainers, same fixture as `test_sqlalchemy_track_repository.py`)
- Adapter: compute `dedup_key(...)` (the slice-3 domain normalizer) from the track's own fields + `user_id`, `INSERT ... ON CONFLICT (user_id, dedup_key) DO NOTHING RETURNING ...`; **when RETURNING is empty, `SELECT` the existing row** and return `created=False`. Map row↔aggregate incl. new fields. Natural idempotency at the DB; `dedup_key` is computed here, never threaded through the domain.
- Regression guard: keep `tests/contract/test_track_repository_contract.py` green.
- Failing tests first: `test_add_persists_new_track_created_true`; `test_add_duplicate_hits_conflict_returns_existing_created_false` (exercises the empty-RETURNING → SELECT branch)
- Verify: `pytest tests/integration/test_track_repository_add.py -v`

### Slice 8: `POST /v1/tracks` endpoint (201 created / 200 existing)
- AC: AC#5, AC#7
- Files:
  - services/api/src/altune/adapters/inbound/http/catalog/router.py (edit)
  - services/api/src/altune/adapters/inbound/http/catalog/dto.py (edit — `CreateTrackRequest`)
  - services/api/tests/e2e/test_tracks_route.py (edit — append POST cases; this is where route coverage lives)
- Adapter: `CreateTrackRequest(title, artist, album?, duration_seconds?, artwork_url?)`; call `AddTrackToLibrary`; `201` when `created`, `200` when dedup hit. `user_id` from `current_user_id`. On the 200/dedup-hit path, increment the dedup-hit metric (spec Telemetry, S6); a fresh save emits `TrackAddedToLibrary` via the use case.
- Failing test first: `test_post_tracks_creates_201_then_dedupes_200`
- Verify: `pytest tests/e2e/test_tracks_route.py -v`

### Slice 9: Extend `GET /v1/tracks` read contract + glossary
- AC: AC#10
- Files:
  - services/api/src/altune/adapters/inbound/http/catalog/dto.py (edit — `TrackResponse` gains `acquisition_status`, `artwork_url`)
  - services/api/src/altune/adapters/inbound/http/catalog/router.py (edit — populate new fields in list mapping)
  - services/api/tests/e2e/test_tracks_route.py (edit — add the GET assertion)
  - docs/ubiquitous-language.md (edit — refresh the `Track` entry only; `AcquisitionStatus`/`TrackAddedToLibrary` already added in slices 1/5)
- Failing test first: `test_list_tracks_includes_acquisition_status`
- Verify: `pytest tests/e2e/test_tracks_route.py -v`

### Slice 10: Mobile api-client — `createTrack` + extended `TrackResponse`
- AC: AC#5, AC#10
- Files:
  - apps/mobile/src/shared/api-client/tracks.ts (edit — `createTrack(body)` POST)
  - apps/mobile/src/shared/api-client/types.ts (edit — `TrackResponse` + `acquisition_status`, `artwork_url`; `CreateTrackRequest`)
  - apps/mobile/src/shared/api-client/__tests__/tracks.test.ts (edit — file already exists; append `createTrack` cases and add `acquisition_status` to the existing `_SAMPLE_TRACK` literal so `getTracks` assertions stay green; do not alter existing `getTracks` tests — sacred-tests)
- Failing test first: `createTrack posts mapped body to /v1/tracks`
- Verify: `npx jest src/shared/api-client/__tests__/tracks.test.ts`

### Slice 11: Detail route + handoff + `DetailScreen` shell
- AC: AC#1, AC#2
- Files:
  - apps/mobile/src/features/detail/detail-handoff.ts (new — module-level last-tapped `SearchResult`)
  - apps/mobile/src/features/detail/ui/DetailScreen.tsx (new — `detail-header`, `detail-back`; circular/square artwork by kind; redirect to /discover when handoff empty)
  - apps/mobile/src/app/detail.tsx (new — sibling of `(tabs)`, renders inside the root `app/_layout.tsx` `<Slot/>`/`AuthGate`, NOT inside the tab group, so the tab bar is hidden)
  - apps/mobile/src/features/detail/__tests__/DetailScreen.test.tsx (new)
- Failing test first: `renders header from handoff; redirects when handoff empty`
- Verify: `npx jest src/features/detail/__tests__/DetailScreen.test.tsx`

### Slice 12: Wire Discover tap → set handoff + navigate (click stays fire-and-forget)
- AC: AC#1
- Files:
  - apps/mobile/src/features/discover/ui/DiscoverScreen.tsx (edit — `onResultTap` sets handoff + `router.push('/detail')`; `click.mutate` unchanged and not awaited)
  - apps/mobile/src/features/discover/__tests__/DiscoverScreen.test.tsx (new or edit)
- Failing test first: `tapping a result records click and pushes /detail without awaiting the click mutation`
- Verify: `npx jest src/features/discover/__tests__/DiscoverScreen.test.tsx`

### Slice 13: Track detail body — `extras` info rows
- AC: AC#3
- Files:
  - apps/mobile/src/features/detail/ui/DetailScreen.tsx (edit — track branch)
  - apps/mobile/src/features/detail/__tests__/DetailScreen.test.tsx (edit)
- UI: render rows only for present `extras` keys — `duration_seconds` (M:SS), `album`, `isrc`, `popularity`; omit absent. **Re-verify key names against the deezer/itunes/musicbrainz adapters before asserting.**
- Failing test first: `track detail shows duration/album/isrc when present, omits absent keys`
- Verify: `npx jest src/features/detail/__tests__/DetailScreen.test.tsx`

### Slice 14: Album & artist detail bodies — placeholders, no Save
- AC: AC#4
- Files:
  - apps/mobile/src/features/detail/ui/DetailScreen.tsx (edit — album/artist branches)
  - apps/mobile/src/features/detail/__tests__/DetailScreen.test.tsx (edit)
- UI: header + available fields + `detail-tracklist-placeholder` / `detail-discography-placeholder`; no `detail-save`. No throw on empty `extras`.
- Failing test first: `album detail shows tracklist placeholder and no save button`
- Verify: `npx jest src/features/detail/__tests__/DetailScreen.test.tsx`

### Slice 15: `useSaveTrack` + Save button (optimistic, rollback, dedup reconcile)
- AC: AC#5, AC#6
- Files:
  - apps/mobile/src/features/detail/hooks/useSaveTrack.ts (new — mutation; optimistic insert into `['library']`; rollback on error; reconcile dedup hit to existing track)
  - apps/mobile/src/features/detail/ui/DetailScreen.tsx (edit — `detail-save`; disabled+loading while pending; error `Banner`)
  - apps/mobile/src/features/detail/__tests__/useSaveTrack.test.ts (new)
  - apps/mobile/src/features/detail/__tests__/DetailScreen.test.tsx (edit)
- Failing test first: `useSaveTrack optimistically inserts then rolls back on error`
- Verify: `npx jest src/features/detail/__tests__/useSaveTrack.test.ts`

### Slice 16: Disable Save when artist (`subtitle`) is null
- AC: AC#9
- Files:
  - apps/mobile/src/features/detail/ui/DetailScreen.tsx (edit — pure render guard)
  - apps/mobile/src/features/detail/__tests__/DetailScreen.test.tsx (edit)
- UI: when the track result's `subtitle` is null/empty, render `detail-save` disabled (the non-empty-artist `Track` invariant can't be met) — no invalid POST.
- Failing test first: `save button disabled when track result has no artist`
- Verify: `npx jest src/features/detail/__tests__/DetailScreen.test.tsx`

### Slice 17: `LibraryRow` renders the `pending` marker
- AC: AC#10
- Files:
  - apps/mobile/src/features/library/ui/LibraryRow.tsx (edit)
  - apps/mobile/src/features/library/__tests__/LibraryRow.test.tsx (new)
- Failing test first: `library row shows pending marker when acquisition_status is pending`
- Verify: `npx jest src/features/library/__tests__/LibraryRow.test.tsx`

## Risks
- **Check-then-insert race (idempotency).** App-level "exists?" then insert races under concurrent Saves / double-submits [vault: wiki/concepts/Idempotency.md]. Mitigation: dedup enforced by the DB `UNIQUE(user_id, dedup_key)` index + `ON CONFLICT` (slices 6–7), never an application pre-check.
- **Normalizer drift.** If the fake and the adapter computed the key differently, dedup would pass in unit tests and fail in prod. Mitigation: one shared `dedup_key` domain function (slice 3) imported by both.
- **God repository.** Keep `TrackRepository` to aggregate-scoped methods (`list_for_user`, `add`) [vault: wiki/concepts/Repository Pattern.md]. No ORM leakage past the adapter.
- **Aggregate bloat.** `acquisition_status` is a VO; `Track` stays small; invariants flow through the root [vault: wiki/concepts/Aggregate.md].
- **Optimistic-UI duplicate.** A Save that turns out to be a dedup hit must reconcile to the existing row, not leave a second optimistic entry [vault: wiki/concepts/Eventual Consistency.md] (slice 15).
- **Extras key drift.** Track `extras` is an untyped wire contract; re-verify `duration_seconds`/`isrc`/`popularity`/`album` against the provider adapters before asserting in slice 13.
- **Cold-start handoff loss.** Empty handoff → redirect to Discover (slice 11); deep-linking is out of scope.

## ADR candidates
- Optional (user decides during review): a short ADR recording "natural idempotency via DB unique constraint for user-scoped saves" as the project default for create endpoints. Minor; no new dependency, so not required.
