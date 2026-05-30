# view-result-detail — session handoff

**Date:** 2026-05-30
**Branch:** `view-result-detail` (branched off `mobile-visual-refresh`, which is now merged to `main`)
**HEAD at handoff:** `2cb20cf`
**Spec:** `docs/specs/view-result-detail/spec.md` · **Plan:** `docs/specs/view-result-detail/plan.md`

---

## TL;DR for the next session

Building the detail-screen feature (17 slices). **Backend slices 1–5 are genuinely green and committed. Slices 6–7 (migration + Postgres `add()`) are committed but RED — two real bugs remain, diagnosed below. Slices 8–17 are untouched.** Start by fixing slices 6–7, then continue.

**Do NOT trust "passed"/"Success" lines without re-running** — this session hit intermittent tool-output corruption where fabricated pass lines appeared. Always re-run a check yourself before believing it. The channel was reliable again by end of session, but stay skeptical: run one command at a time, read the real tail.

---

## Ground truth (verified at handoff)

- **Full unit suite: `216 passed`** [re-verify: `cd services/api && uv run pytest tests/unit -p no:cacheprovider --no-cov -q`]
- **`mypy src`: `Success: no issues found in 86 source files`** [re-verify: `cd services/api && uv run mypy src`]
- **Integration tests for catalog: FAILING** — `test_catalog_migration_dedup.py` and `test_track_repository_add.py` both error/fail. These are committed RED on purpose (TDD red), with fixes still owed.

## What's committed and TRUSTWORTHY (slices 1–5)

| Slice | What | Files |
|---|---|---|
| 1 | `AcquisitionStatus` enum (`PENDING="pending"`) | `services/api/src/altune/domain/catalog/acquisition_status.py` |
| 2 | `Track` gained `artwork_url: str \| None = None`, `acquisition_status = PENDING` | `domain/catalog/track.py` |
| 3 | `dedup_key(title, artist, album)` pure normalizer (casefold + whitespace-collapse, `\x1f` join, null album→"") | `domain/catalog/dedup.py` |
| 4 | `TrackRepository.add` port + in-memory fake (dedup-aware, returns `(track, created)`) | `application/catalog/ports.py`, `tests/_doubles/in_memory_track_repository.py` |
| 5 | `AddTrackToLibrary` use case + `TrackAddedToLibrary` event (emits only on `created=True`) | `application/catalog/add_track_to_library.py`, `domain/catalog/events.py` |

Glossary updated for `AcquisitionStatus` + `TrackAddedToLibrary` in `docs/ubiquitous-language.md`. Commitlint scope `view-result-detail` added to `commitlint.config.js`.

## What's committed but BROKEN (slices 6–7) — fix these first

### Bug A — Postgres `add()` `created` flag is wrong (the core problem)
File: `services/api/src/altune/adapters/outbound/persistence/catalog/track_repository.py`

The committed version uses `INSERT ... ON CONFLICT (user_id, dedup_key) DO NOTHING .returning(TrackRow.id)` then `created = inserted.scalar_one_or_none() is not None`. **This reports `created=True` even on a dedup hit**, because the ORM-entity insert path synthesizes a RETURNING row even when nothing was written. So `test_add_duplicate_returns_existing_created_false` fails its `c2 is False` assertion.

Things tried this session (all dead ends or incomplete):
- `result.rowcount == 1` → mypy: `Result has no attribute rowcount` (need `cast("CursorResult[Any]", ...)` with `from sqlalchemy import CursorResult` under TYPE_CHECKING).
- `pg_insert(TrackRow.__table__)` (Core table instead of ORM entity) → mypy: `Argument 1 to "insert" has incompatible type "FromClause"`. Needs a cast or a different handle on the Table.

**Recommended fix (not yet tried):** keep the ORM-entity insert, but detect dedup by checking whether the inserted `id` equals the row's id, OR — cleaner — drop the "created" cleverness in the adapter and decide `created` in the **use case** by comparing the returned track's `id` to the `id` it generated (`created = returned.id == candidate.id`). The repo just does upsert-returning-canonical. This sidesteps the whole rowcount/RETURNING mess and keeps the dedup signal in one obvious place. Re-verify against `tests/integration/test_track_repository_add.py`.

### Bug B — migration unique-index creation fails on a polluted test DB
File: `migrations/versions/a1c4e7b9d2f3_add_track_acquisition_and_dedup.py`, test `tests/integration/test_catalog_migration_dedup.py`

Error: `could not create unique index "uq_tracks_user_dedup" ... Key (user_id, dedup_key)=(…0001, ) is duplicated`. The migration adds `dedup_key NOT NULL DEFAULT ''` to a `tracks` table that (in the shared testcontainer) already has multiple rows from other tests, all defaulting to empty `dedup_key` → duplicate key → index creation fails.

**Root cause:** integration tests share a module-scoped Postgres container and `tracks` rows leak across modules. The migration itself is fine against an empty table (which is the real pre-launch state). **Fix options:** (1) make the migration test use a truly fresh container/schema (it now pushes `DATABASE_URL` for env.py — verify that's enough, may need a unique DB per module), or (2) the migration should backfill `dedup_key` from title/artist/album for existing rows before adding the unique constraint (more production-honest, and worth doing regardless). Prefer (2) + test isolation.

Note: `track_row.py` has a `_dedup_key_default` column-level default (auto-computes from title/artist/album) so raw `TrackRow(...)` inserts in the *sacred* `test_sqlalchemy_track_repository.py` satisfy NOT NULL — that fix is good and committed, keep it.

## Not started (slices 8–17)

8. `POST /v1/tracks` endpoint — `adapters/inbound/http/catalog/router.py` + `dto.py` (`CreateTrackRequest`). **Note:** there is NO `deps.py` in this dir; the router wires its use case inline via `request.app.state.sessionmaker` (see the existing `get_tracks`). Route tests live in `tests/e2e/test_tracks_route.py` (e2e, not integration). 201 on create, 200 on dedup hit.
9. Extend `GET /v1/tracks` `TrackResponse` with `acquisition_status` + `artwork_url` (so `pending` survives refetch).
10. Mobile `createTrack` + extend `TrackResponse` type — `apps/mobile/src/shared/api-client/tracks.ts` + `types.ts`. **Both files + their test `__tests__/tracks.test.ts` already exist** — EDIT, append cases, add `acquisition_status` to the existing `_SAMPLE_TRACK` literal, don't touch existing `getTracks` assertions (sacred).
11. Detail route + `detail-handoff.ts` (module-level last-tapped SearchResult) + `DetailScreen` shell — new `apps/mobile/src/features/detail/`, route `apps/mobile/src/app/detail.tsx` (sibling of `(tabs)`, renders in root `_layout.tsx` `<Slot/>`, tab bar hidden). Redirect to /discover when handoff empty.
12. Wire Discover `onResultTap` → set handoff + `router.push('/detail')` (click stays fire-and-forget, not awaited) — `apps/mobile/src/features/discover/ui/DiscoverScreen.tsx`.
13. Track detail body — `extras` rows: `duration_seconds` (M:SS), `album`, `isrc`, `popularity`; omit absent. **Re-verify exact extras key names against the deezer/itunes/musicbrainz adapters before asserting.**
14. Album/artist detail bodies — header + `detail-tracklist-placeholder` / `detail-discography-placeholder`, NO save button.
15. `useSaveTrack` mutation — optimistic insert into `['library']` query, rollback on error, reconcile dedup hit to existing track; Save button disabled+loading while pending; error Banner.
16. Disable Save when `subtitle` (artist) null (Track invariant can't be met) — AC#9.
17. `LibraryRow` renders the `pending` marker — `apps/mobile/src/features/library/ui/LibraryRow.tsx`.

Then: `/verify-end-to-end`, `/run`, `/code-review-6-aspect`, `/update-nested-claude-md` (3 backend catalog dirs are flagged — see below), commit.

## Outstanding hygiene debt
- **Stop hook flags 3 missing nested CLAUDE.md:** `domain/catalog/`, `application/catalog/`, `adapters/outbound/persistence/catalog/`. Deferred via `[ALLOW-CLAUDE-MD-DRIFT]` in `2cb20cf`. Run `/update-nested-claude-md <dir>` for each before final merge.
- **Duplicate commit:** `c237b9b` and `7dd8037` are both "add track-repository add port" (slice 4 committed twice). Cosmetic; optional interactive rebase to squash, not worth the risk mid-feature.

## Environment notes / gotchas
- Run backend cmds with **absolute paths, no `cd` chains** — the shell cwd persists, so a second `cd services/api` fails. Use `cd "c:/Users/Alessandro/Desktop/altune/services/api" && ...` fresh each call, or run from repo root.
- Tests need Docker (testcontainers Postgres) — it's available (29.x).
- `uv run pytest` prints a harmless `VIRTUAL_ENV ... does not match` warning — ignore.
- Use `-p no:cacheprovider --no-cov` for clean integration runs.
- Commitlint: header ≤72 chars, subject lowercase, scope must be in the enum (`view-result-detail` is now valid).
- `pre-commit`/`commit-msg` husky hooks are real and DO block — read their full output (don't `>/dev/null` it).
- pytest has no `__init__.py` in test dirs, so **two test files with the same basename collide** (that's why `test_dedup.py` was renamed to `test_catalog_dedup.py`). Keep new test filenames unique repo-wide.

## Architectural decisions locked this session
- Dedup/idempotency = DB `UNIQUE(user_id, dedup_key)` constraint + one shared `dedup_key()` domain normalizer called by both the fake and the adapter (NOT app-level check-then-insert). [vault: wiki/concepts/Idempotency.md]
- `dedup_key` is persistence-only — never a `Track` field, never threaded through the use case.
- Detail screen is fed by an in-memory `SearchResult` handoff, no per-item backend fetch.
- Bigger picture (do not re-litigate): altune is a self-hosted owned-library streaming app; this feature is step 1 of 5 (detail → acquire-track → stream-playback → preview-playback → catalog-browse). See memory `altune-product-vision`.
