# acquire-track — implementation plan

Spec: `docs/specs/acquire-track/spec.md`
Design: `docs/specs/acquire-track/design.md`

## Slices

### Slice 1: FAILED status + failure_reason on Track aggregate
- Acceptance criteria: AC#8 (domain model)
- Files:
  - `services/api/src/altune/domain/catalog/acquisition_status.py` — add `FAILED = "failed"`
  - `services/api/src/altune/domain/catalog/track.py` — add `failure_reason: str | None = None` field, update `__post_init__` invariants: FAILED requires `audio_ref is None`; `failure_reason` non-null only when FAILED; PENDING and READY require `failure_reason is None`
  - `services/api/src/altune/domain/catalog/events.py` — add `TrackAcquisitionCompleted` and `TrackAcquisitionFailed` dataclasses (consumed in slice 16)
  - `services/api/tests/unit/altune/domain/catalog/test_track.py` — tests for FAILED state, invariant interactions
- Failing test first: `test_track_failed_requires_no_audio_ref`, `test_track_failed_requires_failure_reason`
- Verify: `cd services/api && python -m pytest tests/unit/altune/domain/catalog/test_track.py -v`

### Slice 2a: TrackRepository get_by_id + update — port + fake
- Acceptance criteria: AC#7 prerequisite
- Files:
  - `services/api/src/altune/application/catalog/ports.py` — add `get_by_id(track_id: TrackId, user_id: UserId) -> Track | None` and `update(track: Track) -> Track` to `TrackRepository` Protocol
  - `services/api/tests/_doubles/in_memory_track_repository.py` — implement `get_by_id` (lookup by id + user_id filter) and `update` (replace stored Track by id, return new)
  - `services/api/tests/unit/altune/application/catalog/test_in_memory_track_repository.py` — extend existing file with tests for new methods
- Failing test first: `test_get_by_id_returns_track`, `test_get_by_id_wrong_user_returns_none`, `test_update_replaces_track`
- Verify: `cd services/api && python -m pytest tests/unit/altune/application/catalog/test_in_memory_track_repository.py -v`

### Slice 2b: Migration + TrackRow + SQLAlchemy get_by_id/update
- Acceptance criteria: AC#7, AC#8 persistence
- Files:
  - `services/api/migrations/versions/<new>_add_failure_reason.py` — add `failure_reason TEXT NULL` column to `tracks` table
  - `services/api/src/altune/adapters/outbound/persistence/catalog/track_row.py` — add `failure_reason` mapped column, update `from_domain`/`to_domain`
  - `services/api/src/altune/adapters/outbound/persistence/catalog/track_repository.py` — implement `get_by_id` (SELECT by id + user_id) and `update` (load row, overwrite from new Track domain object, flush)
  - `services/api/tests/e2e/test_tracks_route.py` — add test for failure_reason round-trip via GET
- Failing test first: `test_get_by_id_returns_persisted_track`, `test_update_persists_status_change`
- Verify: `cd services/api && python -m pytest tests/e2e/test_tracks_route.py -v`

### Slice 3a: Wire format — failure_reason on TrackResponse
- Acceptance criteria: AC#9 (backend)
- Files:
  - `services/api/src/altune/adapters/inbound/http/catalog/dto.py` — add `failure_reason: str | None = None` to `TrackResponse`
  - `services/api/tests/e2e/test_tracks_route.py` — test: GET returns `failure_reason: null` for normal tracks
- Failing test first: `test_track_response_includes_failure_reason_null`
- Verify: `cd services/api && python -m pytest tests/e2e/test_tracks_route.py -v`

### Slice 3b: Wire format — enrichment fields on CreateTrackRequest
- Acceptance criteria: AC#13 (backend)
- Files:
  - `services/api/src/altune/adapters/inbound/http/catalog/dto.py` — add `isrc: str | None = None`, `year: int | None = None`, `genre: str | None = None`, `album_artist: str | None = None` to `CreateTrackRequest`
  - `services/api/src/altune/application/catalog/add_track_to_library.py` — add `isrc`, `year`, `genre`, `album_artist` to `AddTrackToLibraryInput`, pass to Track construction
  - `services/api/src/altune/adapters/inbound/http/catalog/router.py` — map new fields from request to input
  - `services/api/tests/e2e/test_tracks_route.py` — test: POST with isrc/year/genre → GET returns them
- Failing test first: `test_post_tracks_with_isrc_persists_and_returns`
- Verify: `cd services/api && python -m pytest tests/e2e/test_tracks_route.py -v`

### Slice 4: Mobile metadata enrichment
- Acceptance criteria: AC#13 (mobile)
- Files:
  - `apps/mobile/src/shared/api-client/types.ts` — add `isrc`, `year`, `genre`, `album_artist` (all `string | null` / `number | null`) to `CreateTrackRequest`
  - `apps/mobile/src/features/detail/save-cache.ts` — extend `toCreateTrackRequest` to extract new fields from `result.extras`; update `optimisticTrack` to reflect enrichment fields on the placeholder
  - `apps/mobile/src/features/detail/__tests__/save-cache.test.ts` — test new field extraction + optimistic track
- Failing test first: `test toCreateTrackRequest maps isrc from extras`
- Verify: `cd apps/mobile && npx jest save-cache --verbose`

### Slice 5: MUSIC_DIR config + AudioSearcher/AudioStore ports
- Acceptance criteria: AC#2, AC#6 prerequisites
- Files:
  - `services/api/src/altune/platform/config.py` — add `music_dir: str | None = None` (optional; fail-fast at adapter construction, not at Settings load — avoids breaking existing tests)
  - `services/api/src/altune/application/catalog/ports.py` — add `AudioSearcher` Protocol, `AudioStore` Protocol, `Candidate` plain dataclass (no invariants — just `title: str`, `artist: str`, `duration_seconds: int | None`, `url: str`)
- Failing test first: none — port Protocols and plain dataclasses have no behavior to test. Verified implicitly when adapters (slices 14, 15) implement them.
- Verify: `cd services/api && python -m pytest tests/ -x --timeout=10` (smoke: nothing broken by new definitions)

### Slice 6: Pipeline infrastructure — Step, Context, Runner
- Acceptance criteria: infrastructure (consumed by slices 8–13, 16)
- Files:
  - `services/api/src/altune/application/catalog/acquisition/__init__.py` (new)
  - `services/api/src/altune/application/catalog/acquisition/context.py` (new) — `AcquisitionContext` mutable dataclass
  - `services/api/src/altune/application/catalog/acquisition/pipeline.py` (new) — `Step` Protocol (`execute`/`rollback`), `AcquisitionPipeline` runner
  - `services/api/tests/unit/altune/application/catalog/acquisition/__init__.py` (new)
  - `services/api/tests/unit/altune/application/catalog/acquisition/test_pipeline.py` (new) — test runner with fake steps
- Failing test first: `test_pipeline_executes_steps_in_order`, `test_pipeline_rolls_back_on_failure_in_reverse`
- Verify: `cd services/api && python -m pytest tests/unit/altune/application/catalog/acquisition/test_pipeline.py -v`

### Slice 7: Gate matching logic
- Acceptance criteria: AC#3 (the accuracy core)
- Files:
  - `services/api/src/altune/application/catalog/acquisition/matching.py` (new) — `title_gate`, `artist_gate`, `duration_gate`, `select_best_candidate`
  - Reuses `normalize_for_match` from `application/discovery/normalize.py`. If cross-context import violates architecture rules, extract to a shared location first.
  - `services/api/tests/unit/altune/application/catalog/acquisition/test_matching.py` (new) — comprehensive tests: exact matches, near-matches, rejects, missing duration, "feat." stripping, covers/remixes
- Failing test first: `test_title_gate_rejects_below_085`, `test_select_best_candidate_picks_highest_title_jw`, `test_select_returns_none_when_all_fail`
- Verify: `cd services/api && python -m pytest tests/unit/altune/application/catalog/acquisition/test_matching.py -v`

### Slice 8a: SearchStep
- Acceptance criteria: AC#2 (tiered waterfall)
- Files:
  - `services/api/src/altune/application/catalog/acquisition/steps/__init__.py` (new)
  - `services/api/src/altune/application/catalog/acquisition/steps/search.py` (new) — runs 4-tier waterfall via AudioSearcher port, populates `ctx.candidates`
  - `services/api/tests/unit/altune/application/catalog/acquisition/steps/__init__.py` (new)
  - `services/api/tests/unit/altune/application/catalog/acquisition/steps/test_search.py` (new)
- Failing test first: `test_search_tries_isrc_tier_first`, `test_search_falls_through_to_youtube_on_ytm_miss`
- Verify: `cd services/api && python -m pytest tests/unit/altune/application/catalog/acquisition/steps/test_search.py -v`

### Slice 8b: SelectStep
- Acceptance criteria: AC#3 (gate selection)
- Files:
  - `services/api/src/altune/application/catalog/acquisition/steps/select.py` (new) — applies gate matching from `matching.py`, populates `ctx.selected`
  - `services/api/tests/unit/altune/application/catalog/acquisition/steps/test_select.py` (new)
- Failing test first: `test_select_step_picks_best_passing_candidate`, `test_select_step_raises_no_match_when_all_fail`
- Verify: `cd services/api && python -m pytest tests/unit/altune/application/catalog/acquisition/steps/test_select.py -v`

### Slice 9a: DownloadStep
- Acceptance criteria: AC#4 (download), AC#5 (duration check)
- Files:
  - `services/api/src/altune/application/catalog/acquisition/steps/download.py` (new) — downloads via AudioSearcher port to temp file, runs post-download duration check (log warning if mismatch > 15s)
  - `services/api/tests/unit/altune/application/catalog/acquisition/steps/test_download.py` (new)
- Failing test first: `test_download_step_populates_temp_path`, `test_download_step_logs_duration_mismatch`
- Verify: `cd services/api && python -m pytest tests/unit/altune/application/catalog/acquisition/steps/test_download.py -v`

### Slice 9b: TagStep
- Acceptance criteria: AC#4a (ID3 tagging)
- Files:
  - `services/api/src/altune/application/catalog/acquisition/steps/tag.py` (new) — writes ID3v2.4 tags via mutagen: title, artist, album, year, track_number, album_artist, genre. Fetches + embeds album art from artwork_url.
  - `services/api/tests/unit/altune/application/catalog/acquisition/steps/test_tag.py` (new) — test with a real temp MP3 fixture (small silent file)
- Failing test first: `test_tag_step_writes_title_and_artist`, `test_tag_step_embeds_album_art`
- Verify: `cd services/api && python -m pytest tests/unit/altune/application/catalog/acquisition/steps/test_tag.py -v`
- Note: `mutagen` is a standard pure-Python library for audio metadata; no ADR needed.

### Slice 10: StoreStep + UpdateTrackStep
- Acceptance criteria: AC#6 (file storage), AC#7 (Track → READY)
- Files:
  - `services/api/src/altune/application/catalog/acquisition/steps/store.py` (new) — moves file via AudioStore port, populates `ctx.audio_ref`
  - `services/api/src/altune/application/catalog/acquisition/steps/update_track.py` (new) — loads Track, creates new instance with READY + audio_ref, persists via repo
  - `services/api/tests/unit/altune/application/catalog/acquisition/steps/test_store.py` (new)
  - `services/api/tests/unit/altune/application/catalog/acquisition/steps/test_update_track.py` (new)
- Failing test first: `test_store_step_calls_audio_store_and_sets_ref`, `test_update_track_step_transitions_to_ready`
- Verify: `cd services/api && python -m pytest tests/unit/altune/application/catalog/acquisition/steps/test_store.py tests/unit/altune/application/catalog/acquisition/steps/test_update_track.py -v`

### Slice 11: YtDlpAudioSearcher adapter
- Acceptance criteria: AC#2 (search), AC#4 (download)
- Files:
  - `services/api/src/altune/adapters/outbound/audio/__init__.py` (new)
  - `services/api/src/altune/adapters/outbound/audio/ytdlp_searcher.py` (new) — implements AudioSearcher via yt-dlp `extract_info` (search) and `download` (MP3 320kbps)
  - `services/api/tests/_doubles/fake_audio_searcher.py` (new) — in-memory test double
  - `services/api/tests/integration/test_ytdlp_searcher.py` (new) — integration test (requires yt-dlp + FFmpeg + network; marked `@pytest.mark.integration`)
- Failing test first: `test_ytdlp_searcher_returns_candidates_for_known_track`
- Verify: `cd services/api && python -m pytest tests/integration/test_ytdlp_searcher.py -v` (CI note: requires network + yt-dlp)

### Slice 12: FilesystemAudioStore adapter
- Acceptance criteria: AC#6 (file storage)
- Files:
  - `services/api/src/altune/adapters/outbound/audio/filesystem_store.py` (new) — sanitizes filenames, creates user/artist/album dirs, moves file, returns relative path
  - `services/api/tests/_doubles/fake_audio_store.py` (new) — in-memory test double
  - `services/api/tests/unit/altune/adapters/outbound/audio/test_filesystem_store.py` (new) — test with `tmp_path`
- Failing test first: `test_filesystem_store_creates_user_artist_album_path`, `test_filesystem_store_sanitizes_filenames`
- Verify: `cd services/api && python -m pytest tests/unit/altune/adapters/outbound/audio/test_filesystem_store.py -v`

### Slice 13: AcquireTrackAudio use case
- Acceptance criteria: AC#1 (orchestration), AC#10 (idempotency), AC#12 (cleanup)
- Files:
  - `services/api/src/altune/application/catalog/acquisition/acquire_track_audio.py` (new) — constructs 6-step pipeline, runs it. On failure: sets Track to FAILED + reason. Emits domain events (from slice 1). Temp cleanup in `finally`. Skips if Track is already READY.
  - `services/api/tests/unit/altune/application/catalog/acquisition/test_acquire_track_audio.py` (new) — scenarios: success (PENDING → READY), failure-no-match (PENDING → FAILED), failure-download-error, idempotency skip (already READY), temp cleanup on failure
- Failing test first: `test_acquire_transitions_pending_to_ready`, `test_acquire_sets_failed_on_no_match`, `test_acquire_skips_already_ready`
- Verify: `cd services/api && python -m pytest tests/unit/altune/application/catalog/acquisition/test_acquire_track_audio.py -v`

### Slice 14: HTTP trigger — BackgroundTasks wiring
- Acceptance criteria: AC#1, AC#11 (auto-trigger, survives request lifecycle)
- Files:
  - `services/api/src/altune/adapters/inbound/http/catalog/router.py` — add `BackgroundTasks` param, spawn `AcquireTrackAudio.execute` when `output.created is True`. Background task creates its own session from `app.state.sessionmaker`.
  - `services/api/src/altune/platform/wiring.py` — add `build_audio_searcher(cfg)` and `build_audio_store(cfg)` helpers
  - `services/api/src/altune/platform/app.py` — wire searcher + store into `app.state` in lifespan
  - `services/api/tests/e2e/test_acquisition_trigger.py` (new) — e2e: POST /v1/tracks → verify acquisition was triggered (mock use case)
- Failing test first: `test_post_tracks_triggers_acquisition_on_create`, `test_post_tracks_dedup_does_not_trigger`
- Verify: `cd services/api && python -m pytest tests/e2e/test_acquisition_trigger.py tests/e2e/test_tracks_route.py -v`

### Slice 15: Mobile — failed state UI
- Acceptance criteria: AC#9 (mobile rendering)
- Files:
  - `apps/mobile/src/shared/api-client/types.ts` — add `failure_reason: string | null` to `TrackResponse`
  - `apps/mobile/src/features/library/ui/LibraryRow.tsx` — handle `acquisition_status === 'failed'` with error indicator (icon/text)
  - `apps/mobile/src/features/library/__tests__/LibraryRow.test.tsx` — test failed state rendering
- Failing test first: `test LibraryRow shows error indicator for failed tracks`
- Verify: `cd apps/mobile && npx jest LibraryRow --verbose`

## Shippable checkpoints

- **After slice 3b + 4:** Metadata enrichment is live. ISRC, year, genre, album_artist flow from discovery → Track. Independently valuable — improves data quality regardless of acquisition.
- **After slice 15:** Full feature is live end-to-end.

## Risks

- **yt-dlp network dependency.** Integration tests (slice 11) require network + yt-dlp + FFmpeg. Marked `@pytest.mark.integration`, excluded from default `pytest` run. Unit tests use `FakeAudioSearcher`.
- **normalize_for_match cross-context import.** Slice 7 imports from `application/discovery/normalize.py`. If this violates architecture rules, extract to a shared location first. Decision made during slice 7 implementation.
- **Frozen Track + update.** Track is `frozen=True` — the SQLAlchemy `update` method (slice 2b) must load the row, overwrite attributes from the new Track instance, and flush. Not mutate the domain object.
- **BackgroundTasks session scope.** The background task runs outside the HTTP session. The use case (slice 13) creates its own session via `sessionmaker`. This is wired in the router (slice 14).
- **Steps must not leak adapter concerns.** DownloadStep and TagStep operate through ports, never directly importing yt-dlp or mutagen. Hexagonal boundary discipline.

## ADR candidates

- None. yt-dlp, FFmpeg, and mutagen are standard tools, not architectural commitments.
