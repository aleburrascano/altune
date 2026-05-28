# discover-music-v1 — implementation plan

Spec: [docs/specs/discover-music-v1/spec.md](spec.md)
ADR:  [docs/adr/0007-unified-music-search.md](../../adr/0007-unified-music-search.md)
Brainstorm: [docs/brainstorms/2026-05-27-unified-music-search.md](../../brainstorms/2026-05-27-unified-music-search.md)

> **47 engineering slices** (each 2–5 min, RED→GREEN→REFACTOR, every slice ships independently), plus a 4-item **pre-feature operational checklist** and a 3-item **post-feature checklist**. Spine-first: one-provider end-to-end (Slice 17) before scatter-gather (Slice 21+); scatter-gather before resiliency; resiliency before cache; backend before mobile. Glossary updates are interleaved with the domain slices that introduce each term (B7 from the plan-reviewer). Operational items live in checklists, not slices, so the TDD invariant holds for engineering work (B2 from the plan-reviewer).

## Acceptance criteria restated

24 ACs in the spec (AC#1, 2, 3, 4, 5, 5a, 5b, 6, 7, 7a, 8, 9, 10, 10a, 11, 11a, 12, 13, 14, 15, 16, 17, 18, 19, 20). Plain-language re-read:

- **Search returns merged ranked results across 4 providers** (AC#1, 19, ranking spec) with a wire shape that's open-closed against future providers.
- **Dedup uses ISRC when present, JW ≥ 0.85 with a confidence-label split at 0.92 otherwise**, with low-confidence results staying separate (AC#2, 3, 4).
- **Resiliency**: partial-result tolerance for per-source timeout / 5xx / 429 / circuit-open (AC#5, 5a, 5b, 6); 4-source bulkhead via per-source `httpx.AsyncClient`.
- **Caching** in Redis, per-source TTL, full-warm and mixed-warm cases, version-prefix invalidation, post-ACL canonical-shape cache (AC#7, 7a, 8, 9).
- **URL-paste**: 4 supported hosts route directly; unsupported hosts fall through to text (AC#10, 10a).
- **Search history**: persisted per-search, ring-buffer 50, read-side distinct-by-`query_norm` top-10, multi-tenant isolated (AC#11, 11a, 12, 13, 14).
- **Click tracking**: await-before-202; sliding-window 60s dedup against the last persisted click (AC#15, 16).
- **Validation + auth**: 422 on out-of-range, 401 on missing/invalid token (AC#17, 18).
- **Mobile**: five mutually-exclusive view states with stable testIDs; partial-result is `results + partial-banner`, not an error (AC#20).

## Vault grounding

- `[vault: wiki/concepts/Vertical Slice Architecture.md]` — feature-cohesion organization, traversing all layers per slice.
- `[vault: wiki/concepts/Test Double.md]` — `InMemorySearchProvider` is a Fowler-stub (controllable canned responses); `InMemorySearchHistoryRepository` is a fake (working in-memory implementation). The `RedisQueryCache` integration test uses testcontainers (real Redis, not a mock).
- `[vault: wiki/concepts/Anti-Corruption Layer Pattern.md]` — translation stays in the adapter; the use case sees domain types only.
- `[vault: wiki/concepts/Enterprise Integration Patterns.md]` — Scatter-Gather + Aggregator + Normalizer applied selectively (no message bus).
- `[vault: wiki/concepts/Circuit Breaker Pattern.md]` — per-source isolation; no retry; observable state transitions.
- `[vault: wiki/concepts/Hexagonal Architecture.md]` — domain pure; ports in application; adapters compose at the platform layer.
- `[vault: wiki/concepts/Idempotency.md]` — server-side window-based dedup on `/v1/discovery/clicks`; safe-idempotent GET on search.

## Pre-feature operational checklist

These are setup actions, not engineering slices — they have no failing tests because they have no production code. Complete them before Slice 1.

- **C1: Provider account registrations.** ~~Register the altune SoundCloud developer app~~ (superseded — see ADR-0007 strategy revision; SoundCloud via yt-dlp needs no registration); register a Last.fm API account (capture `LASTFM_API_KEY`); choose the prod Redis host (Upstash free-tier likely; capture `REDIS_URL`); finalize MusicBrainz User-Agent string + contact email. Commit `services/api/.env.example` placeholders. Commit: `chore(discover-music-v1): document discovery provider env vars`.
- **C2: Add runtime deps.** `uv add redis rapidfuzz yt-dlp` (confirm `respx` already a dev dep). Verify: `uv run python -c "from redis.asyncio import Redis; from rapidfuzz import fuzz; import yt_dlp; print('ok')"`. Commit: `chore(deps): add redis + rapidfuzz + yt-dlp for discover-music-v1`.
- **C3: Add `redis:7-alpine` to `docker-compose.yml`.** With healthcheck + port 6379. Verify: `docker compose up -d redis && docker compose ps redis` shows `(healthy)` within 10s. Commit: `chore(tooling): add redis service to local docker-compose`.
- **C4: Capture provider fixtures.** One-shot live call against each of the 4 providers (one query per (provider, kind) — e.g., `the beatles` against Deezer track-search, MB recording-search, SC track-search, Last.fm track.search) → freeze the JSON into `tests/integration/fixtures/discovery/<provider>/<kind>.json`. Used by `respx`-mocked adapter integration tests. Commit: `test(discover-music-v1): freeze provider fixtures for respx mocking`.

## Slices

### Phase 1 — Foundation (settings + migrations)

#### Slice 1: `Settings` gains discovery + Redis fields with per-field validators
- Acceptance: AC#18 transitively; ADR-0007 settings contract
- Files:
  - `services/api/src/altune/platform/config.py`
  - `services/api/tests/unit/altune/platform/test_config.py`
- Failing tests first (named, parameterized over fields):
  - `test_settings_accepts_well_formed_redis_url`
  - `test_settings_rejects_malformed_redis_url`
  - `test_settings_lastfm_api_key_is_secret_str_and_round_trips`
  - `test_settings_rejects_musicbrainz_user_agent_without_contact_form_or_email`
- Verify: `uv run pytest tests/unit/altune/platform/test_config.py -v`
- Commit: `feat(platform): wire discover-music-v1 settings + redis config`

#### Slice 2: First Alembic migration — `discovery_search_history` table
- Acceptance: AC#11, 12, 13, 14
- Files: `services/api/alembic/versions/<rev>_add_discovery_search_history.py`
- Failing test first: `tests/integration/altune/migrations/test_discovery_search_history_migration.py::test_migration_creates_table_with_expected_columns_and_index`
- Verify: `uv run pytest tests/integration/.../test_discovery_search_history_migration.py -v`
- Commit: `feat(adapters): add discovery_search_history alembic migration`

#### Slice 3: Second Alembic migration — `discovery_search_clicks` table
- Acceptance: AC#15, 16
- Files: `services/api/alembic/versions/<rev>_add_discovery_search_clicks.py`
- Failing test first: `tests/integration/.../test_discovery_search_clicks_migration.py::test_migration_creates_table_with_confidence_check_constraint`
- Verify: `uv run pytest tests/integration/.../test_discovery_search_clicks_migration.py -v`
- Commit: `feat(adapters): add discovery_search_clicks alembic migration`

### Phase 2 — Domain primitives (glossary updates interleaved per B7)

#### Slice 4: `ResultKind` + `Confidence` value objects + glossary entries
- Acceptance: AC#1 wire shape; powers ranking
- Files:
  - `services/api/src/altune/domain/discovery/__init__.py`
  - `services/api/src/altune/domain/discovery/result_kind.py` — enum or `Literal`
  - `services/api/src/altune/domain/discovery/confidence.py` — enum with comparison (HIGH > MEDIUM > LOW)
  - `services/api/tests/unit/altune/domain/discovery/test_confidence.py`
  - **`docs/ubiquitous-language.md` gains `ResultKind` and `Confidence` Canonical entries in the same commit** (prevents `terminology-drift` stop-hook from blocking Slice 5+)
- Failing test first: `test_confidence_orders_high_above_medium_above_low`
- Verify: `uv run pytest tests/unit/altune/domain/discovery/test_confidence.py -v`
- Commit: `feat(domain): add discovery ResultKind + Confidence + glossary`

#### Slice 5: `ProviderName` enum + `SourceRef` value object + glossary
- Acceptance: AC#1 sources-array shape
- Files:
  - `services/api/src/altune/domain/discovery/provider.py` — `ProviderName.{DEEZER, MUSICBRAINZ, SOUNDCLOUD, LASTFM}`
  - `services/api/src/altune/domain/discovery/source_ref.py` — `@dataclass(frozen=True) class SourceRef: provider: ProviderName; external_id: str; url: str`
  - tests
  - `docs/ubiquitous-language.md` gains `SourceRef` (canonical) + footnote on `ProviderName`
- Failing test first: `test_source_ref_is_frozen_and_compares_by_value`
- Verify: `uv run pytest tests/unit/altune/domain/discovery/test_source_ref.py -v`
- Commit: `feat(domain): add discovery ProviderName + SourceRef + glossary`

#### Slice 6: `SearchResult` aggregate + glossary (no `preview_url` invariant — that's adapter-layer per B8)
- Acceptance: AC#1 result-entry shape; AC#3 multi-source merge
- Files:
  - `services/api/src/altune/domain/discovery/search_result.py` — `@dataclass(frozen=True) class SearchResult: kind: ResultKind; title: str; subtitle: str | None; image_url: str | None; confidence: Confidence; sources: tuple[SourceRef, ...]; extras: Mapping[str, object]`
  - Invariants in `__post_init__`: `title` non-empty; `sources` non-empty tuple
  - tests
  - `docs/ubiquitous-language.md` gains `SearchResult`
- Failing tests first:
  - `test_search_result_rejects_empty_title`
  - `test_search_result_rejects_empty_sources_tuple`
  - `test_search_result_equals_by_value`
- Verify: `uv run pytest tests/unit/altune/domain/discovery/test_search_result.py -v`
- Commit: `feat(domain): add SearchResult aggregate + glossary`

#### Slice 7: `SearchQuery` value object + `ProviderStatus` enum + glossary
- Acceptance: AC#1, 5, 5a, 5b, 6, 17
- Files:
  - `services/api/src/altune/domain/discovery/search_query.py` — invariants: `raw` non-empty, `1 ≤ limit ≤ 50`, `kinds` non-empty frozenset
  - `services/api/src/altune/domain/discovery/provider_status.py` — `OK / TIMEOUT / ERROR / RATE_LIMITED / CIRCUIT_OPEN`
  - tests
  - `docs/ubiquitous-language.md` gains `SearchQuery` + `ProviderStatus`
- Failing tests first: `test_search_query_rejects_limit_above_50`, `test_search_query_rejects_empty_kinds`
- Verify: `uv run pytest tests/unit/altune/domain/discovery/ -v`
- Commit: `feat(domain): add SearchQuery + ProviderStatus + glossary`

#### Slice 8: `SearchHistoryEntry` + `SearchClick` aggregates + glossary
- Acceptance: AC#11, 15
- Files:
  - `services/api/src/altune/domain/discovery/search_history_entry.py`
  - `services/api/src/altune/domain/discovery/search_click.py`
  - tests
  - `docs/ubiquitous-language.md` gains `SearchHistoryEntry` + `SearchClick`
- Failing tests first: `test_search_history_entry_equals_by_id`, `test_search_click_position_must_be_non_negative`
- Verify: `uv run pytest tests/unit/altune/domain/discovery/ -v`
- Commit: `feat(domain): add SearchHistoryEntry + SearchClick + glossary`

#### Slice 9: Domain events (`SearchPerformed`, `ResultClicked`)
- Acceptance: ADR-0007 domain events; powers Phase-distributed telemetry
- Files: `services/api/src/altune/domain/discovery/events.py` — frozen past-tense dataclasses with `occurred_at` field
- Failing test first: `test_search_performed_is_immutable_with_occurred_at`
- Verify: `uv run pytest tests/unit/altune/domain/discovery/test_events.py -v`
- Commit: `feat(domain): add discovery domain events`

#### Slice 10: Promote `Artist` / `Album` / `Playlist` from Future → Canonical in glossary
- Acceptance: Dependencies section of spec; required before any adapter starts emitting these terms
- Files: `docs/ubiquitous-language.md`
- Verify: `git diff docs/ubiquitous-language.md` shows the three terms moved with v1-shaped definitions
- Commit: `docs(spec): canonicalize Artist/Album/Playlist for discover-music-v1`

### Phase 3 — Application primitives (normalization split per S6)

#### Slice 11a: `normalize_for_match()` — NFKC + lowercase + diacritic-strip
- Acceptance: AC#3 dedup correctness (first three rules)
- Files:
  - `services/api/src/altune/application/discovery/__init__.py`
  - `services/api/src/altune/application/discovery/normalize.py`
  - `services/api/tests/unit/altune/application/discovery/test_normalize.py`
- Failing tests first: `test_normalize_nfkc_collapses_fullwidth_to_halfwidth`, `test_normalize_lowercases`, `test_normalize_strips_diacritics_on_beyonce`
- Verify: `uv run pytest tests/unit/altune/application/discovery/test_normalize.py -v -k "nfkc or lowercase or diacrit"`
- Commit: `feat(application): add normalize_for_match nfkc+lower+diacritics`

#### Slice 11b: `normalize_for_match()` — bracket-strip + feature-notation
- Acceptance: AC#3 dedup correctness (rules 4–5)
- Files: extend `normalize.py` + tests
- Failing tests first: `test_normalize_drops_bracketed_remastered_suffix`, `test_normalize_unifies_feat_ft_featuring_to_feat`
- Verify: `uv run pytest tests/unit/altune/application/discovery/test_normalize.py -v -k "bracket or feat"`
- Commit: `feat(application): add bracket-strip + feature-notation normalization`

#### Slice 11c: `normalize_for_match()` — leading-article + punct/space-collapse + trim
- Acceptance: AC#3 dedup correctness (rules 6–8)
- Files: extend `normalize.py` + tests + hypothesis property tests on the full pipeline
- Failing tests first: `test_normalize_strips_leading_the_from_artist`, `test_normalize_collapses_punctuation_and_whitespace`, `test_normalize_property_idempotent`
- Verify: `uv run pytest tests/unit/altune/application/discovery/test_normalize.py -v`
- Commit: `feat(application): finish normalize_for_match (article + punct + property tests)`

#### Slice 12: `SearchProvider` port + `ProviderSearchResponse` DTO
- Acceptance: AC#1, ADR-0007 SearchProvider signature
- Files:
  - `services/api/src/altune/application/discovery/ports.py` — `Protocol` for `SearchProvider`; `ProviderSearchResponse` carries `(provider, status, results: tuple[SearchResult, ...], latency_ms)`
- Failing test first: `tests/unit/altune/application/discovery/test_ports.py::test_search_provider_protocol_is_runtime_checkable`
- Verify: `uv run pytest tests/unit/altune/application/discovery/test_ports.py -v` + `uv run mypy services/api/src/altune/application/discovery/ports.py`
- Commit: `feat(application): define SearchProvider port`

#### Slice 13: `InMemorySearchProvider` + `QueryCache` port + `InMemoryQueryCache`
- Acceptance: enables fast unit testing of dedup + use case without real providers
- Files:
  - `services/api/tests/_doubles/in_memory_search_provider.py`
  - `services/api/src/altune/application/discovery/ports.py` (extend with `QueryCache`)
  - `services/api/tests/_doubles/in_memory_query_cache.py`
- Failing test first: `test_in_memory_search_provider_returns_canned_results_with_configurable_latency`
- Verify: `uv run pytest tests/_doubles/ -v`
- Commit: `feat(application): add QueryCache port + in-memory test doubles`

#### Slice 14: `dedup_and_rank()` — ISRC + JW merge + multi-criteria ranking
- Acceptance: AC#2, 3, 4 + ranking section of spec
- Files:
  - `services/api/src/altune/application/discovery/dedup.py` — pure function `dedup_and_rank(results: Sequence[SearchResult]) -> tuple[SearchResult, ...]`; uses `rapidfuzz.distance.JaroWinkler.normalized_similarity`
  - tests with explicit boundary cases at JW=0.84, 0.85, 0.91, 0.92
- Failing tests first:
  - `test_dedup_merges_isrc_matched_into_high_confidence`
  - `test_dedup_collapses_jw_above_92_into_high`
  - `test_dedup_collapses_jw_in_85_to_92_into_medium`
  - `test_dedup_keeps_jw_below_85_separate_as_low`
  - `test_dedup_uses_winning_per_source_prior_for_canonical_representative`
  - `test_ranking_orders_by_confidence_then_multi_source_bool_then_prior_then_alpha`
- Verify: `uv run pytest tests/unit/altune/application/discovery/test_dedup.py -v`
- Commit: `feat(application): add ISRC+JW dedup + multi-criteria ranking`

#### Slice 15: `SearchHistoryRepository` + `SearchClickRepository` ports + fakes
- Acceptance: AC#11, 12, 13, 14, 15, 16
- Files:
  - `services/api/src/altune/application/discovery/ports.py` (extend)
  - `services/api/tests/_doubles/in_memory_search_history_repository.py` (supports ring-buffer trimming + distinct-recent listing)
  - `services/api/tests/_doubles/in_memory_search_click_repository.py` (supports sliding-window dedup)
- Failing tests first: `test_in_memory_history_repo_trims_to_50_on_insert`, `test_in_memory_click_repo_dedupes_within_60s_of_last_persist`
- Verify: `uv run pytest tests/_doubles/ -v`
- Commit: `feat(application): add SearchHistory + SearchClick repository ports`

#### Slice 16: `SearchMusic` use case (one-provider skeleton, no cache, no scatter-gather)
- Acceptance: spine for AC#1
- Files:
  - `services/api/src/altune/application/discovery/search_music.py` — calls a single `SearchProvider`, runs `dedup_and_rank`, persists history, returns response DTO
  - tests using `InMemorySearchProvider` + `InMemorySearchHistoryRepository`
- Failing test first: `test_search_music_returns_dedup_ranked_results_and_persists_history`
- Verify: `uv run pytest tests/unit/altune/application/discovery/test_search_music.py -v -k spine`
- Commit: `feat(application): add SearchMusic use case (one-provider skeleton)`

### Phase 4 — One-provider end-to-end (Deezer is simplest; no auth)

#### Slice 17: `DeezerSearchAdapter` (ACL) + tolerant-reader (`provider_response_malformed` log event)
- Acceptance: AC#1, AC#19 (tolerant reader on Deezer)
- Files:
  - `services/api/src/altune/adapters/outbound/discovery/deezer/adapter.py`
  - `respx` fixtures from C4
- Failing tests first:
  - `test_deezer_adapter_translates_track_search_response_with_isrc_in_extras`
  - `test_deezer_adapter_drops_malformed_result_missing_title_and_emits_log_event`
- Verify: `uv run pytest tests/integration/altune/adapters/outbound/discovery/deezer/ -v`
- Commit: `feat(adapters): add Deezer ACL adapter with tolerant reader`

#### Slice 18: `GET /v1/discovery/search` endpoint — happy + validation matrix + 401 auth (S2: merge of prior 21/22/23)
- Acceptance: AC#1, AC#17, AC#18
- Files:
  - `services/api/src/altune/adapters/inbound/http/discovery/__init__.py`
  - `services/api/src/altune/adapters/inbound/http/discovery/router.py`
  - `services/api/src/altune/adapters/inbound/http/discovery/schemas.py` (Pydantic response models)
  - `services/api/src/altune/platform/app.py` (register router)
  - `services/api/tests/e2e/altune/discovery/test_search_endpoint.py`
- Failing tests first (parameterized matrix):
  - `test_search_endpoint_returns_200_with_dedup_results`
  - `test_search_returns_422_for_invalid_inputs[q_empty]`
  - `test_search_returns_422_for_invalid_inputs[limit_0]`
  - `test_search_returns_422_for_invalid_inputs[limit_51]`
  - `test_search_returns_422_for_invalid_inputs[kinds_invalid]`
  - `test_search_returns_422_for_invalid_inputs[kinds_empty]`
  - `test_search_returns_401_without_bearer_token`
  - `test_search_returns_401_with_invalid_token`
- Verify: `uv run pytest tests/e2e/altune/discovery/test_search_endpoint.py -v`
- Commit: `feat(adapters): wire GET /v1/discovery/search with validation + auth matrix`

### Phase 5 — Multi-provider scatter-gather (Slice 21 split per B6)

#### Slice 19: `MusicBrainzSearchAdapter` (ACL) — required User-Agent + tolerant-reader log
- Acceptance: AC#1, AC#19
- Files: `services/api/src/altune/adapters/outbound/discovery/musicbrainz/adapter.py`
- Failing tests first:
  - `test_musicbrainz_adapter_sends_configured_user_agent`
  - `test_musicbrainz_adapter_translates_recording_search_with_isrc`
  - `test_musicbrainz_adapter_drops_malformed_and_logs_provider_response_malformed`
- Verify: `uv run pytest tests/integration/altune/adapters/outbound/discovery/musicbrainz/ -v`
- Commit: `feat(adapters): add MusicBrainz ACL adapter`

#### Slice 20: `LastFmSearchAdapter` (ACL)
- Acceptance: AC#1, AC#19
- Files: `services/api/src/altune/adapters/outbound/discovery/lastfm/adapter.py`
- Failing tests first: `test_lastfm_adapter_sends_api_key_in_querystring`, `test_lastfm_adapter_translates_track_search`, `test_lastfm_adapter_drops_malformed`
- Verify: `uv run pytest tests/integration/altune/adapters/outbound/discovery/lastfm/ -v`
- Commit: `feat(adapters): add Last.fm ACL adapter`

#### Slice 21: `SoundCloudSearchAdapter` (ACL) via yt-dlp `scsearch:` in `asyncio.to_thread`
- Acceptance: AC#1 (SC tracks in `sources[]`), AC#19 (tolerant reader on yt-dlp entries), ADR-0007 strategy revision
- Files:
  - `services/api/src/altune/adapters/outbound/discovery/soundcloud/adapter.py` — wraps `yt_dlp.YoutubeDL.extract_info(f"scsearch{limit}:{query}")` in `asyncio.to_thread`; translates yt-dlp entries to `SearchResult` with `uploader|channel|uploader_id` as artist, no ISRC, `confidence: low` baseline (per-source prior 0.65). Matches the legacy adapter's translation shape [VERIFIED:Read@C:\Users\Alessandro\music-manager\backend\providers\soundcloud_provider.py#L91-L100].
- Failing tests first:
  - `test_soundcloud_adapter_extracts_via_scsearch_prefix`
  - `test_soundcloud_adapter_falls_back_through_uploader_channel_uploader_id_for_artist`
  - `test_soundcloud_adapter_picks_largest_thumbnail`
  - `test_soundcloud_adapter_drops_yt_dlp_entries_missing_required_fields_and_logs`
  - `test_soundcloud_adapter_runs_in_to_thread_does_not_block_event_loop`
- Verify: `uv run pytest tests/integration/altune/adapters/outbound/discovery/soundcloud/ -v`
- Commit: `feat(adapters): add SoundCloud ACL adapter via yt-dlp scsearch`

#### Slice 22a: Scatter-gather fan-out across 4 providers via `asyncio.TaskGroup` (B6 part 1)
- Acceptance: AC#1 (4 providers in flight in parallel)
- Files: `services/api/src/altune/application/discovery/search_music.py` — replace single-provider call with TaskGroup fan-out
- Failing test first: `test_search_music_calls_all_four_providers_in_parallel_within_p95_below_serial_sum`
- Verify: `uv run pytest tests/unit/altune/application/discovery/test_search_music.py -v -k parallel`
- Commit: `feat(application): scatter-gather across 4 providers`

#### Slice 22b: Per-source `asyncio.wait_for` timeout → `ProviderStatus.TIMEOUT` partial-result (B6 part 2)
- Acceptance: AC#5 — the subtle TaskGroup-cancellation-on-sibling-failure pitfall (plan-reviewer's risk callout)
- Files: extend `search_music.py`; each provider call wrapped in `wait_for(call, 1.5)` with `try/except TimeoutError: → ProviderStatus.TIMEOUT`
- Failing tests first:
  - `test_search_music_returns_partial_on_one_provider_timeout`
  - `test_search_music_continues_when_one_provider_raises_mid_flight`
  - `test_search_music_does_not_cancel_siblings_on_one_failure`
- Verify: `uv run pytest tests/unit/altune/application/discovery/test_search_music.py -v -k timeout`
- Commit: `feat(application): per-source timeout + sibling-safe partial results`

#### Slice 23: 5xx-error partial-result handling (AC#5a)
- Acceptance: AC#5a
- Files: adapter-side error mapping (5xx → `ProviderStatus.ERROR`) + use-case test
- Failing test first: `test_search_music_returns_partial_on_provider_5xx_with_error_status`
- Verify: `uv run pytest tests/unit/altune/application/discovery/test_search_music.py -v -k error_partial`
- Commit: `feat(application): map provider 5xx to partial-error response`

#### Slice 24: 429 rate-limit handling (AC#5b) — distinct from `ERROR`
- Acceptance: AC#5b — must NOT count toward circuit breaker per spec
- Files: adapter-side 429 → `ProviderStatus.RATE_LIMITED`
- Failing test first: `test_search_music_returns_partial_on_provider_429_distinct_status_does_not_count_toward_breaker`
- Verify: `uv run pytest tests/unit/altune/application/discovery/test_search_music.py -v -k rate_limited`
- Commit: `feat(application): map provider 429 to rate_limited partial response`

### Phase 6 — Resiliency: circuit breakers + bulkhead

#### Slice 25: Per-source `CircuitBreaker` state machine + `circuit_breaker_state_change` log event (S5)
- Acceptance: AC#6
- Files:
  - `services/api/src/altune/application/discovery/circuit_breaker.py` — pure state machine
  - tests for state transitions + log emission
- Failing tests first:
  - `test_breaker_opens_after_5_consecutive_failures`
  - `test_breaker_half_opens_after_30s_via_injected_clock`
  - `test_breaker_emits_state_change_log_event_on_open`
  - `test_rate_limited_does_not_count_toward_breaker_failures`
- Verify: `uv run pytest tests/unit/altune/application/discovery/test_circuit_breaker.py -v`
- Commit: `feat(application): add per-source circuit breaker with state-change logs`

#### Slice 26: Integrate `CircuitBreaker` into `SearchMusic` scatter-gather
- Acceptance: AC#6
- Files: `services/api/src/altune/application/discovery/search_music.py`
- Failing test first: `test_search_music_skips_provider_when_breaker_open_and_returns_partial_with_circuit_open_status`
- Verify: `uv run pytest tests/unit/altune/application/discovery/test_search_music.py -v -k breaker`
- Commit: `feat(application): wire circuit breaker into scatter-gather`

#### Slice 27: Per-source `httpx.AsyncClient` bulkhead (DI wiring)
- Acceptance: ADR-0007 bulkhead stance
- Files: `services/api/src/altune/platform/app.py` — lifespan creates 4 separate `AsyncClient`s; injects each adapter with its own client
- Failing test first: `tests/unit/altune/platform/test_app.py::test_each_provider_adapter_owns_its_own_async_client_instance` (asserts distinct identity per S4)
- Verify: `uv run pytest tests/unit/altune/platform/test_app.py -v -k bulkhead`
- Commit: `feat(platform): bulkhead per-provider AsyncClient pools`

### Phase 7 — Caching

#### Slice 28: `RedisQueryCache` adapter (full-warm) + `cache_unavailable` log event (S5)
- Acceptance: AC#7 (full-warm), AC#8 (cache write)
- Files:
  - `services/api/src/altune/adapters/outbound/discovery/cache/redis_cache.py` — implements `QueryCache`; serializes `SearchResult[]` as JSON; per-source TTL
  - `services/api/tests/integration/altune/adapters/outbound/discovery/cache/test_redis_cache.py` — `testcontainers` Redis
- Failing tests first:
  - `test_redis_cache_round_trips_search_result_list`
  - `test_redis_cache_respects_per_source_ttl`
  - `test_redis_cache_treats_redis_error_as_miss_and_logs_cache_unavailable`
- Verify: `uv run pytest tests/integration/altune/adapters/outbound/discovery/cache/ -v`
- Commit: `feat(adapters): add Redis query-cache adapter with per-source TTL`

#### Slice 29: Cache-aware `SearchMusic` — full-warm hit + cache write on miss
- Acceptance: AC#7, AC#8
- Files: `services/api/src/altune/application/discovery/search_music.py`
- Failing tests first:
  - `test_search_music_hits_cache_when_all_sources_warm_no_live_calls`
  - `test_search_music_writes_to_cache_on_miss_with_per_source_ttl`
- Verify: `uv run pytest tests/unit/altune/application/discovery/test_search_music.py -v -k cache`
- Commit: `feat(application): cache-aware full-warm path in SearchMusic`

#### Slice 30: Per-source mixed-warm cache path (AC#7a — load-bearing)
- Acceptance: AC#7a
- Files: same; per-source cache lookup happens BEFORE scatter-gather; only expired sources go live
- Failing tests first (cover both common bug-shapes):
  - `test_search_music_uses_cached_for_warm_sources_and_live_for_expired`
  - `test_search_music_does_not_treat_any_cached_source_as_full_hit_when_one_expired`
  - `test_search_music_does_not_refetch_all_when_only_one_expired`
- Verify: `uv run pytest tests/unit/altune/application/discovery/test_search_music.py -v -k mixed_warm`
- Commit: `feat(application): per-source mixed-warm cache lookup`

#### Slice 31: Cache version-prefix (AC#9)
- Acceptance: AC#9
- Files: `redis_cache.py` adds version constant; key builder uses it
- Failing tests first: `test_cache_key_includes_v1_prefix_and_kinds_sorted_csv`, `test_cache_with_v2_prefix_does_not_see_v1_entries`
- Verify: `uv run pytest tests/unit/altune/adapters/outbound/discovery/cache/test_cache_key.py -v`
- Commit: `feat(adapters): add cache version-prefix for non-destructive invalidation`

### Phase 8 — URL-paste resolution (B4 split into 32–36)

#### Slice 32: `url_router.py` — pure regex matcher
- Acceptance: AC#10 (detection), AC#10a (fall-through detection)
- Files:
  - `services/api/src/altune/application/discovery/url_router.py` — `match_provider(url: str) -> ProviderName | None`
  - tests
- Failing tests first:
  - `test_url_router_matches_deezer_track_url`
  - `test_url_router_matches_musicbrainz_recording_url`
  - `test_url_router_matches_soundcloud_track_url`
  - `test_url_router_matches_lastfm_url`
  - `test_url_router_returns_none_for_unsupported_host_spotify`
  - `test_url_router_returns_none_for_unsupported_host_example`
- Verify: `uv run pytest tests/unit/altune/application/discovery/test_url_router.py -v`
- Commit: `feat(application): add url_router pure regex matcher`

#### Slice 33: Deezer `lookup_by_url` + `SearchMusic` short-circuit branch
- Acceptance: AC#10 happy path on Deezer; AC#10a fall-through to text
- Files:
  - `deezer/adapter.py` — implement `lookup_by_url`
  - `search_music.py` — add `LookupByUrl` branch
- Failing tests first:
  - `test_deezer_lookup_by_url_returns_single_high_confidence_result`
  - `test_search_music_short_circuits_on_supported_deezer_url`
  - `test_search_music_falls_through_to_text_on_unsupported_url`
- Verify: `uv run pytest tests/integration/altune/adapters/outbound/discovery/deezer/ tests/unit/altune/application/discovery/test_search_music.py -v -k url`
- Commit: `feat(application): deezer lookup_by_url + url-paste branch`

#### Slice 34: MusicBrainz `lookup_by_url`
- Acceptance: AC#10
- Files: `musicbrainz/adapter.py`
- Failing test first: `test_musicbrainz_lookup_by_url_resolves_recording_mbid_from_url`
- Verify: `uv run pytest tests/integration/altune/adapters/outbound/discovery/musicbrainz/ -v -k url`
- Commit: `feat(adapters): musicbrainz lookup_by_url`

#### Slice 35: SoundCloud `lookup_by_url` (uses `/resolve?url=` endpoint)
- Acceptance: AC#10
- Files: `soundcloud/adapter.py`
- Failing test first: `test_soundcloud_lookup_by_url_resolves_via_resolve_endpoint`
- Verify: `uv run pytest tests/integration/altune/adapters/outbound/discovery/soundcloud/ -v -k url`
- Commit: `feat(adapters): soundcloud lookup_by_url via resolve endpoint`

#### Slice 36: Last.fm `lookup_by_url`
- Acceptance: AC#10
- Files: `lastfm/adapter.py`
- Failing test first: `test_lastfm_lookup_by_url_extracts_artist_track_from_url`
- Verify: `uv run pytest tests/integration/altune/adapters/outbound/discovery/lastfm/ -v -k url`
- Commit: `feat(adapters): lastfm lookup_by_url`

### Phase 9 — Search history

#### Slice 37: `SqlAlchemySearchHistoryRepository` — insert + trim + distinct-recent
- Acceptance: AC#11, 12, 13, 14
- Files:
  - `services/api/src/altune/adapters/outbound/persistence/discovery/search_history_repository.py` — `insert(entry)`, `trim_to_n(user_id, n=50)`, `list_distinct_recent(user_id, limit=10)`
  - integration test against `testcontainers` Postgres
- Failing tests first:
  - `test_repo_inserts_history_row`
  - `test_repo_trims_to_50_on_insert_past_threshold`
  - `test_repo_list_distinct_recent_groups_by_query_norm_returning_most_recent_per_group`
  - `test_repo_isolates_users_by_user_id` (covers AC#14)
- Verify: `uv run pytest tests/integration/altune/adapters/outbound/persistence/discovery/test_search_history_repository.py -v`
- Commit: `feat(adapters): add SqlAlchemySearchHistoryRepository`

#### Slice 38: Wire history persist into `SearchMusic` + best-effort fallback (AC#11, 11a) — S8 strengthened
- Acceptance: AC#11, AC#11a
- Files: `services/api/src/altune/application/discovery/search_music.py`
- Failing tests first (parameterized over the exception types to inject):
  - `test_search_music_persists_history_on_full_200_response`
  - `test_search_music_persists_history_on_partial_200_response`
  - `test_search_music_returns_200_when_history_insert_raises_OperationalError`
  - `test_search_music_returns_200_when_history_insert_raises_IntegrityError`
  - `test_search_music_emits_search_history_persist_failed_log_on_failure`
- Verify: `uv run pytest tests/unit/altune/application/discovery/test_search_music.py -v -k history`
- Commit: `feat(application): persist search history best-effort on every 200`

#### Slice 39: `GET /v1/discovery/search-history` endpoint (AC#13) + multi-tenancy e2e (AC#14)
- Acceptance: AC#13, AC#14
- Files:
  - `discovery/router.py` — new route
  - `application/discovery/list_search_history.py` (new use case)
  - e2e test
- Failing tests first:
  - `test_history_endpoint_returns_distinct_top_10_newest_first`
  - `test_history_endpoint_with_zero_history_returns_empty_items_total_zero`
  - `test_history_endpoint_isolates_users_with_explicit_literal_uuids`
- Verify: `uv run pytest tests/e2e/altune/discovery/test_search_history_endpoint.py -v`
- Commit: `feat(adapters): add GET /v1/discovery/search-history endpoint`

### Phase 10 — Click tracking (B5 split into 40–42)

#### Slice 40: `SqlAlchemySearchClickRepository` — sliding-window dedup against last persist
- Acceptance: AC#15 persistence, AC#16 sliding-window
- Files:
  - `services/api/src/altune/adapters/outbound/persistence/discovery/search_click_repository.py` — `insert_if_outside_window(click, window_seconds=60)` returns enum `Inserted | Deduped`
  - integration test
- Failing tests first:
  - `test_repo_inserts_click_outside_window`
  - `test_repo_dedupes_identical_click_within_60s_of_last_persist`
  - `test_repo_persists_when_60s_elapsed_since_last_persist`
- Verify: `uv run pytest tests/integration/altune/adapters/outbound/persistence/discovery/test_search_click_repository.py -v`
- Commit: `feat(adapters): add SqlAlchemySearchClickRepository`

#### Slice 41: `RecordClick` use case + `result_signature` deterministic computation
- Acceptance: AC#15 use case logic, AC#16 dedup wiring, spec §"result_signature definition"
- Files:
  - `services/api/src/altune/application/discovery/record_click.py` — computes `result_signature = sha256(f"{kind}|{normalize_for_match(title)}|{normalize_for_match(subtitle or '')}")[:12]`
  - unit tests using `InMemorySearchClickRepository`
- Failing tests first:
  - `test_record_click_computes_result_signature_with_normalize_for_match`
  - `test_record_click_returns_inserted_outcome_outside_window`
  - `test_record_click_returns_deduped_outcome_within_window`
  - `test_record_click_signature_collapses_diacritic_variants_to_same_hash`
- Verify: `uv run pytest tests/unit/altune/application/discovery/test_record_click.py -v`
- Commit: `feat(application): add RecordClick use case + result_signature`

#### Slice 42: `POST /v1/discovery/clicks` endpoint — await-before-202 (AC#15)
- Acceptance: AC#15 endpoint behavior, AC#16 e2e idempotency
- Files:
  - `discovery/router.py` — new route returning 202 Accepted (empty body) after awaited persistence
  - e2e test against `testcontainers` Postgres + in-process app
- Failing tests first:
  - `test_clicks_endpoint_returns_202_with_empty_body_after_persist`
  - `test_clicks_endpoint_followup_db_query_immediately_after_202_sees_the_row` (await-before-202 assertion)
  - `test_clicks_endpoint_dedupes_identical_payload_within_60s_returns_202_still`
- Verify: `uv run pytest tests/e2e/altune/discovery/test_clicks_endpoint.py -v`
- Commit: `feat(adapters): add POST /v1/discovery/clicks with await-before-202`

### Phase 11 — Mobile feature slice

#### Slice 43: `shared/api-client/discovery.ts` typed client
- Acceptance: backend contract from AC#1, AC#10, AC#13, AC#15
- Files:
  - `apps/mobile/src/shared/api-client/discovery.ts` — `searchDiscovery(q, kinds, limit)`, `lookupByUrl(url)`, `recordClick(payload)`, `listSearchHistory()`
  - `apps/mobile/src/shared/api-client/types.ts` (extend) — mirror response types
  - jest tests
- Failing tests first:
  - `test_searchDiscovery_passes_kinds_csv_in_querystring`
  - `test_recordClick_posts_json_payload_and_handles_202`
  - `test_listSearchHistory_returns_typed_items_array`
- Verify: `cd apps/mobile && npm test src/shared/api-client/__tests__/discovery.test.ts`
- Commit: `feat(mobile): add typed discovery API client`

#### Slice 44: `features/discover/state.ts` — `_viewForState` + `_shouldShowPartialBanner` pure helpers
- Acceptance: AC#20 view states (precise mapping per S2 from spec-reviewer)
- Files:
  - `apps/mobile/src/features/discover/state.ts` — `_viewForState(hookState, query) → 'loading' | 'empty-no-query' | 'results' | 'zero-results' | 'full-error'` + `_shouldShowPartialBanner(response) → boolean`
  - jest tests
- Failing tests first:
  - `test_view_for_state_returns_empty_no_query_when_query_is_blank`
  - `test_view_for_state_returns_loading_when_query_present_and_data_undefined`
  - `test_view_for_state_returns_zero_results_on_200_with_empty_results`
  - `test_view_for_state_returns_results_on_200_with_items`
  - `test_view_for_state_returns_full_error_on_5xx`
  - `test_partial_banner_shows_when_any_provider_not_ok`
  - `test_partial_banner_hidden_when_all_providers_ok`
- Verify: `cd apps/mobile && npm test src/features/discover/__tests__/state.test.ts`
- Commit: `feat(mobile): add discover feature state machine helpers`

#### Slice 45: `useDiscoverSearch` + `useSearchHistory` + `useRecordClick` hooks
- Acceptance: AC#20 (hook drives view state); AC#13 (history empty-state); AC#15 (click POST)
- Files:
  - `apps/mobile/src/features/discover/hooks/useDiscoverSearch.ts`
  - `apps/mobile/src/features/discover/hooks/useSearchHistory.ts`
  - `apps/mobile/src/features/discover/hooks/useRecordClick.ts`
- Failing tests first:
  - `test_useDiscoverSearch_does_not_fire_when_query_empty`
  - `test_useSearchHistory_returns_distinct_top_10`
  - `test_useRecordClick_posts_payload_with_required_fields`
- Verify: `cd apps/mobile && npm test src/features/discover/__tests__/hooks/ -- --watchAll=false`
- Commit: `feat(mobile): add discover React Query hooks`

#### Slice 46: `DiscoverScreen.tsx` + row + partial banner with all five testIDs (AC#20)
- Acceptance: AC#20 — testIDs `discover-loading`, `discover-empty-no-query` + `discover-history-row-<idx>`, `discover-results` + `discover-row-<sig>`, `discover-zero-results`, `discover-full-error` + `discover-retry`, `discover-partial-banner`
- Files:
  - `apps/mobile/src/features/discover/ui/DiscoverScreen.tsx`
  - `apps/mobile/src/features/discover/ui/DiscoverRow.tsx`
  - `apps/mobile/src/features/discover/ui/PartialBanner.tsx`
  - component tests
- Failing tests first:
  - `test_DiscoverScreen_renders_discover_loading_in_initial_state`
  - `test_DiscoverScreen_renders_discover_empty_no_query_with_history_rows_when_blank`
  - `test_DiscoverScreen_renders_discover_results_with_partial_banner_sibling_when_response_partial_true`
  - `test_DiscoverScreen_renders_discover_zero_results_on_empty_200`
  - `test_DiscoverScreen_renders_discover_full_error_with_retry_on_5xx`
- Verify: `cd apps/mobile && npm test src/features/discover/__tests__/DiscoverScreen.test.tsx`
- Commit: `feat(mobile): add DiscoverScreen with five-state view machine`

### Phase 12 — End-to-end validation

#### Slice 47: Full e2e happy-path with all 4 providers + Redis + Postgres (`respx` + testcontainers)
- Acceptance: AC#1 end-to-end with realistic fixtures + AC#5/5a/5b/6/7/7a partial-failure matrix
- Files: `tests/e2e/altune/discovery/test_full_search_flow.py`
- Failing tests first:
  - `test_search_round_trip_4_providers_redis_postgres_returns_dedup_ranked_response`
  - `test_search_round_trip_with_one_provider_timeout_returns_partial_response`
  - `test_search_round_trip_with_one_provider_circuit_open_returns_partial_response`
  - `test_search_round_trip_with_mixed_cache_state_uses_warm_skips_live`
- Verify: `uv run pytest tests/e2e/altune/discovery/ -v`
- Commit: `test(discover-music-v1): full e2e happy-path + partial-fail matrix`

## Post-feature checklist

- **C5: Mount `DiscoverScreen` at the chosen Expo Router path.** UX surface — tab vs modal vs persistent bar — chosen during this step with `ux-reviewer` subagent gating. Files: `apps/mobile/src/app/discover.tsx` (or `discover/index.tsx` per UX choice) + `apps/mobile/src/features/discover/CLAUDE.md` (new — mirrors `library/CLAUDE.md`). Verify: `cd apps/mobile && npx expo start --clear` and manually navigate to the surface; visual confirmation; component tests already passed in Slice 46. Commit: `feat(mobile): mount DiscoverScreen at /<route>`.
- **C6: `/verify-end-to-end` clean.** Runs typecheck + lint + unit + integration + e2e per skill flow. Address any failures before C7.
- **C7: `/code-review-6-aspect` clean + spec status flip.** Dispatches the 6 parallel reviewers; resolve 🚨 BLOCK findings. Then edit `docs/specs/discover-music-v1/spec.md` status → Shipped. Commit: `docs(spec): mark discover-music-v1 shipped`.

## Risks

Lifted from spec's Risks section + vault anti-patterns + slice-planning observations:

- **Vault anti-pattern (Test Double):** mocking external HTTP services with hand-authored shapes yields tests that pass with adapter bugs. Mitigation: all 4 provider adapters' integration tests use `respx` against **frozen JSON captured from one-shot live calls** (pre-feature C4), not hand-authored shapes.
- **Vault anti-pattern (Circuit Breaker):** silent state changes (Closed → Open → Half-Open) make incidents hard to diagnose. Mitigation: Slice 25 emits `circuit_breaker_state_change` log events as part of the breaker contract.
- **Vault anti-pattern (Vertical Slice Architecture):** the "horizontal layer" trap — committing "all of domain" then "all of application" then "all of adapters" — would push the feature's first end-to-end demo to the last day. Mitigation: spine ships at Slice 17 (Deezer one-provider); each subsequent slice extends a working spine. Phase 2 has six consecutive domain slices that look horizontal but are unblocking the spine (defensible because each domain type is independently testable and consumed in Slice 14 / Slice 16).
- **Slice 22b risk (TaskGroup cancellation):** `asyncio.TaskGroup`'s behavior when a child raises is to cancel siblings — we want siblings to complete normally and collect partial-results. The implementation must convert per-task exceptions into `ProviderStatus.{TIMEOUT,ERROR,RATE_LIMITED}` results inside the task body (try/except), NOT let them bubble. Slice 22b's tests explicitly cover the "1 of 4 raises mid-flight" scenario.
- **Slice 21 risk (yt-dlp brittleness + sync wrapping):** yt-dlp is synchronous and depends on SoundCloud's public web surface — both the adapter's `asyncio.to_thread` discipline AND its tolerance to HTML-shape drift matter. The per-source circuit breaker (Slice 25) and per-source `wait_for(1500ms)` (Slice 22b) are the safety net; under sustained yt-dlp failure the breaker trips SC out of the rotation without affecting other providers. ToS posture is the same as the legacy `music-manager` (no incident in >18 months of operation).
- **Slice 30 risk (mixed-warm cache):** two opposite common bugs — (a) "any cached source counts as full hit" and (b) "any expired source forces refetch of all". Slice 30 names both as explicit failing tests so the implementer can't accidentally pick the wrong branch.
- **Slice 40 risk (sliding-window idempotency race):** two concurrent identical POSTs may both find no recent row and both insert. v1 stance: app-level race acceptable (rare, low harm); future spec adds a unique partial index with `ON CONFLICT DO NOTHING` if telemetry shows duplicates.
- **Slice 46 risk (partial-banner UX subtlety):** AC#20 specifies `results + partial-banner` siblings, not `partial-error` instead of results. Slice 44's pure helper pins this precisely; Slice 46's component test asserts both nodes render simultaneously.
- **Bundle-time env-var risk (mobile):** `EXPO_PUBLIC_*` is baked at bundle time per `apps/mobile/src/features/library/CLAUDE.md`. Keep mobile-side config minimal; backend owns most knobs.
- **Glossary/terminology-drift hook risk:** the `terminology-drift` stop-hook will block commits introducing domain types not in `docs/ubiquitous-language.md`. Mitigation: each domain slice (4–10) updates the glossary in the same commit (B7 fix).

## ADR candidates that emerged during planning

None new — ADR-0007 already covers every architectural decision the slices materialize. The plan is downstream of a locked ADR.

## Out of plan (explicit non-deliverables)

- Sentry / crash reporting integration. Future cross-cutting spec.
- Metrics ADR (request count, p95 latency). Future ADR + spec; this plan emits structured logs that a metrics layer can later derive.
- OpenAPI codegen for the mobile client. Hand-mirrored types in this plan (per `view-library` precedent); codegen is its own future spec.
- E2E test runner (Maestro / Detox) on mobile. Component tests via jest-expo + RNTL only in this plan.
- Region-locking, NSFW filtering, romanization — all spec'd Out of Scope.
