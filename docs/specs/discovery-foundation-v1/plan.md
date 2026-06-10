# discovery-foundation-v1 — implementation plan

Spec: docs/specs/discovery-foundation-v1/spec.md

Phases are ordered so each depends only on prior phases: eval harness first (measure before changing), entity resolution (fix identity), signal scoring (fix ranking), quality gates (fix validation), then bug fixes (cleanup).

## Phase 0: Eval Harness

### Slice 1: Expand golden cases — ambiguous and short-name queries
- Acceptance criterion: AC#15 (partial — ambiguous category)
- Files:
  - `services/api/tests/eval/golden_cases.py` (edit — add ~10 cases for "Che", "X", "Low", "M", "Bush", "Seal", "Sia", "Banks", "Mika", "Hurt")
- Failing test first: `test_golden_case_count_minimum` — assert `len(GOLDEN_CASES) >= 50` fails (currently 10)
- Verify: `pytest services/api/tests/eval/test_ranking_quality.py -v -k "count"`

### Slice 2: Expand golden cases — partial, misspelled, nonsense, and cover traps
- Acceptance criterion: AC#15 (remaining categories to reach ≥50)
- Files:
  - `services/api/tests/eval/golden_cases.py` (edit — add ~30 cases: partial/misspelled like "playboi cart", "daft pnk"; nonsense like "asdfghjkl123", "xyzzy"; cover/karaoke traps like "Shape of You" where genuine must outrank "Shape of You (Karaoke)")
- Failing test first: `test_golden_cases_cover_all_categories` — assert each of 5 categories has ≥3 cases
- Verify: `pytest services/api/tests/eval/test_ranking_quality.py -v -k "categories"`

### Slice 3: Baseline MRR@10 snapshot + regression assertion
- Acceptance criterion: AC#16, AC#17
- Files:
  - `services/api/tests/eval/test_ranking_quality.py` (edit — add baseline snapshot assertion: run eval, record MRR@10, assert no regression vs snapshot)
- Failing test first: `test_mrr_does_not_regress_below_baseline` — assert current MRR@10 ≥ recorded baseline
- Verify: `pytest services/api/tests/eval/test_ranking_quality.py -v`

## Phase 1: Entity Resolution

### Slice 4: EntityResolutionTier domain type + glossary
- Acceptance criterion: foundation for AC#1-6
- Files:
  - `services/api/src/altune/domain/discovery/entity_resolution_tier.py` (new — enum: MBID, ISRC, DURATION_CONFIRMED, FUZZY, NONE with ordering)
  - `services/api/tests/unit/altune/domain/discovery/test_entity_resolution_tier.py` (new)
  - `docs/ubiquitous-language.md` (edit — add EntityResolutionTier, QualityScore, ContentValidationStatus)
- Failing test first: `test_entity_resolution_tier_ordering` — MBID > ISRC > DURATION_CONFIRMED > FUZZY > NONE
- Verify: `pytest services/api/tests/unit/altune/domain/discovery/test_entity_resolution_tier.py -v`

### Slice 5: Refactor `_try_merge` return type to carry resolution tier
- Acceptance criterion: foundation for AC#1-6 (structural change)
- Files:
  - `services/api/src/altune/application/discovery/dedup.py` (edit — `_try_merge` returns `tuple[SearchResult, EntityResolutionTier] | None` instead of `SearchResult | None`; `fuse_and_rank` updated to unpack the new return; resolution tier stored in `extras["resolution_tier"]` on the merged result)
  - `services/api/tests/unit/altune/application/discovery/test_dedup.py` (edit — existing tests updated for new return shape)
- Failing test first: `test_try_merge_returns_resolution_tier` — ISRC merge returns `(merged_result, EntityResolutionTier.ISRC)`
- Verify: `pytest services/api/tests/unit/altune/application/discovery/test_dedup.py -v`

### Slice 6: MBID merge with title validation gate
- Acceptance criterion: AC#1, AC#6
- Files:
  - `services/api/src/altune/application/discovery/dedup.py` (edit — MBID branch adds JW ≥ 0.85 title check; returns MBID tier on match, None on title disagreement)
  - `services/api/tests/unit/altune/application/discovery/test_dedup.py` (edit — add MBID+title-agree and MBID+title-disagree cases)
- Failing test first: `test_mbid_merge_rejected_when_titles_diverge` — two results with same MBID but JW < 0.85 remain separate
- Verify: `pytest services/api/tests/unit/altune/application/discovery/test_dedup.py -v -k "mbid"`

### Slice 7: Different-MBID separation
- Acceptance criterion: AC#2
- Files:
  - `services/api/src/altune/application/discovery/dedup.py` (edit — `_try_merge` rejects when both results have MBIDs and they differ, before falling through to JW similarity)
  - `services/api/tests/unit/altune/application/discovery/test_dedup.py` (edit — add different-MBID-same-name case; first verify current behavior — if already separated by JW artist threshold, the test passes immediately and this is a guard-rail slice)
- Failing test first: `test_different_mbids_same_name_remain_separate` — two "Che" artists with different MBIDs stay as two results
- Verify: `pytest services/api/tests/unit/altune/application/discovery/test_dedup.py -v -k "different_mbid"`

### Slice 8: ISRC-tier merge regardless of MBID presence
- Acceptance criterion: AC#3
- Files:
  - `services/api/src/altune/application/discovery/dedup.py` (edit — ISRC match path assigns ISRC resolution tier, works when one result has MBID and other doesn't)
  - `services/api/tests/unit/altune/application/discovery/test_dedup.py` (edit)
- Failing test first: `test_isrc_merge_when_one_has_mbid_other_does_not` — ISRC match merges at ISRC tier
- Verify: `pytest services/api/tests/unit/altune/application/discovery/test_dedup.py -v -k "isrc_tier"`

### Slice 9: Duration-confirmed merge tier
- Acceptance criterion: AC#4, AC#5
- Files:
  - `services/api/src/altune/application/discovery/dedup.py` (edit — add duration check in fuzzy-match path: `extras["duration_seconds"]` within 15s → DURATION_CONFIRMED tier; >15s → reject; missing duration → fall through to FUZZY)
  - `services/api/tests/unit/altune/application/discovery/test_dedup.py` (edit — 3 cases: duration match, duration mismatch, duration missing)
- Failing test first: `test_duration_mismatch_blocks_merge` — same title, durations 180s vs 220s, remain separate
- Verify: `pytest services/api/tests/unit/altune/application/discovery/test_dedup.py -v -k "duration"`

## Phase 2: Signal-Based Quality Scoring

### Slice 10: QualityScore value object + scorer port
- Acceptance criterion: foundation for AC#8
- Files:
  - `services/api/src/altune/domain/discovery/quality_score.py` (new — value object, composite [0,1], breakdown fields: completeness, agreement, entity_tier, fetch_success)
  - `services/api/src/altune/application/discovery/ports.py` (edit — add FetchSuccessStore protocol: `get_rate(provider, external_id) -> float`)
  - `services/api/tests/unit/altune/domain/discovery/test_quality_score.py` (new)
- Failing test first: `test_quality_score_normalized_to_unit_interval` — score outside [0,1] raises ValueError
- Verify: `pytest services/api/tests/unit/altune/domain/discovery/test_quality_score.py -v`

### Slice 11: Quality scorer — metadata completeness + multi-source + entity tier signals
- Acceptance criterion: AC#8 (partial — three of four signals)
- Files:
  - `services/api/src/altune/application/discovery/quality_scorer.py` (new — computes completeness from extras fields, counts distinct providers in sources, maps entity resolution tier to signal value)
  - `services/api/tests/unit/altune/application/discovery/test_quality_scorer.py` (new)
- Failing test first: `test_multi_source_result_scores_higher_than_single_source` — result with 3 sources + ISRC scores above single-source result without ISRC (no fetch success rate involved yet)
- Verify: `pytest services/api/tests/unit/altune/application/discovery/test_quality_scorer.py -v`

### Slice 12: Fetch success rate signal — port + in-memory fake + scorer integration
- Acceptance criterion: AC#8 (partial — fourth signal, application layer only)
- Files:
  - `services/api/src/altune/application/discovery/quality_scorer.py` (edit — add fetch_success_rate signal from FetchSuccessStore port; defaults to 1.0 when no history exists)
  - `services/api/tests/unit/altune/application/discovery/test_quality_scorer.py` (edit — test with in-memory FetchSuccessStore fake)
- Failing test first: `test_fetch_success_rate_lowers_score` — result with 0.5 fetch success rate scores below identical result with 1.0 rate
- Verify: `pytest services/api/tests/unit/altune/application/discovery/test_quality_scorer.py -v -k "fetch_success"`

### Slice 13: Redis FetchSuccessStore adapter
- Acceptance criterion: AC#8 (storage for fourth signal)
- Files:
  - `services/api/src/altune/adapters/outbound/discovery/cache/fetch_success_store.py` (new — Redis per `(provider, external_id)`, sliding window of 10 attempts, 48h TTL)
  - `services/api/tests/integration/altune/adapters/outbound/discovery/test_fetch_success_store.py` (new)
- Failing test first: `test_fetch_success_rate_decreases_after_failure` — record 5 successes then 5 failures, rate drops from 1.0 to 0.5
- Verify: `pytest services/api/tests/integration/altune/adapters/outbound/discovery/test_fetch_success_store.py -v`

### Slice 14: Verify metadata-sparse result scores below rich result
- Acceptance criterion: AC#9
- Files:
  - `services/api/tests/unit/altune/application/discovery/test_quality_scorer.py` (edit — end-to-end scoring test: sparse vs rich result)
- Failing test first: `test_sparse_single_source_scores_below_rich_multi_source` — no-ISRC, no-image, single-source, query-in-title result scores lower than multi-source result with ISRC + image
- Verify: `pytest services/api/tests/unit/altune/application/discovery/test_quality_scorer.py -v -k "sparse"`

### Slice 15: Replace sort key — quality score replaces prior + multi_source
- Acceptance criterion: AC#10
- Files:
  - `services/api/src/altune/application/discovery/dedup.py` (edit — `fuse_and_rank` accepts a quality scorer; sort key becomes `(-band, demoted, bootleg, -quality_score, -popularity, -rrf, subtitle, title)`; `_PRIORS` usage removed from sort key)
  - `services/api/tests/unit/altune/application/discovery/test_fuse_and_rank.py` (edit — update sort-key expectations)
- Failing test first: `test_sort_key_uses_quality_score_not_priors` — verify quality_score position in sort key
- Verify: `pytest services/api/tests/unit/altune/application/discovery/test_fuse_and_rank.py -v`

### Slice 16: Wire quality scorer into search orchestration
- Acceptance criterion: AC#8 (integration)
- Files:
  - `services/api/src/altune/application/discovery/search_music.py` (edit — SearchMusic receives QualityScorer; passes it to fuse_and_rank; DI wiring note: platform/wiring.py must construct QualityScorer with FetchSuccessStore)
  - `services/api/tests/unit/altune/application/discovery/test_search_music.py` (edit — provide mock scorer)
- Failing test first: `test_search_music_passes_scorer_to_fuse_and_rank` — verify scorer is called during search
- Verify: `pytest services/api/tests/unit/altune/application/discovery/test_search_music.py -v -k "scorer"`

### Slice 17: Build signal-based demotion replacement
- Acceptance criterion: foundation for AC#7 (builds the replacement logic BEFORE removing constants)
- Files:
  - `services/api/src/altune/application/discovery/quality_scorer.py` (edit — add `is_likely_junk(result, query_norm) -> bool` using metadata signals: no-ISRC + no-image + single-source + title-contains-full-query. Add `is_likely_cover(result) -> bool` using: record_type in extras, provider-specific metadata. These replace `_is_demoted` and `_is_bootleg`)
  - `services/api/src/altune/application/discovery/dedup.py` (edit — `_is_demoted` and `_is_bootleg` delegate to quality scorer instead of keyword lists; `_passes_gate` uses relevance_score > 0 instead of `_content_tokens` + stopwords)
  - `services/api/tests/unit/altune/application/discovery/test_fuse_and_rank.py` (edit — verify karaoke/tribute results still demoted via signals, not keywords)
- Failing test first: `test_demotion_works_via_signals_not_keywords` — result with record_type="compilation" + single-source + no-ISRC is demoted even without "karaoke" in title
- Verify: `pytest services/api/tests/unit/altune/application/discovery/test_fuse_and_rank.py -v -k "demotion"`

### Slice 18: Remove static constants
- Acceptance criterion: AC#7
- Files:
  - `services/api/src/altune/application/discovery/dedup.py` (edit — remove `_PRIORS`, `_JUNK_TITLE_MARKERS`, `_JUNK_TITLE_WORD_RE`, `_DEMOTED_RECORD_TYPES`, `_STOPWORDS` module-level constants; all call sites now use quality scorer signals from slice 17)
  - `services/api/tests/unit/altune/application/discovery/test_fuse_and_rank.py` (edit — add grep-style assertion confirming constants are gone)
- Failing test first: `test_no_static_priors_or_junk_markers_in_module` — assert `_PRIORS`, `_JUNK_TITLE_MARKERS`, `_STOPWORDS` not in `dir(dedup)`
- Verify: `pytest services/api/tests/unit/altune/application/discovery/ -v` (full suite — catch any breakage from removal)
- Note: run eval harness after this slice to confirm no MRR regression

## Phase 3: Quality Gates

### Slice 19: ContentValidationStatus domain type + port
- Acceptance criterion: foundation for AC#11-14
- Files:
  - `services/api/src/altune/domain/discovery/content_validation_status.py` (new — enum: FETCHABLE, UNFETCHABLE, UNKNOWN)
  - `services/api/src/altune/application/discovery/ports.py` (edit — add ContentValidationCache protocol: `get(provider, external_id) -> ContentValidationStatus`, `record(provider, external_id, status) -> None`)
  - `services/api/tests/unit/altune/domain/discovery/test_content_validation_status.py` (new)
- Failing test first: `test_content_validation_status_members` — enum has exactly FETCHABLE, UNFETCHABLE, UNKNOWN
- Verify: `pytest services/api/tests/unit/altune/domain/discovery/test_content_validation_status.py -v`

### Slice 20: Content validation recording in GetAlbumTracks
- Acceptance criterion: AC#14 (partial — recording in album fetch)
- Files:
  - `services/api/src/altune/application/discovery/get_album_tracks.py` (edit — accept ContentValidationCache + FetchSuccessStore ports; record FETCHABLE on success, UNFETCHABLE on content-fetch error; update FetchSuccessStore)
  - `services/api/tests/unit/altune/application/discovery/test_get_album_tracks.py` (edit — verify validation + fetch success recorded)
- Failing test first: `test_album_tracks_fetch_records_validation_success` — after successful fetch, ContentValidationCache.record called with FETCHABLE and FetchSuccessStore.record called with success=True
- Verify: `pytest services/api/tests/unit/altune/application/discovery/test_get_album_tracks.py -v -k "validation"`

### Slice 21: Content validation recording in GetArtistContent
- Acceptance criterion: AC#14 (partial — recording in artist fetch)
- Files:
  - `services/api/src/altune/application/discovery/get_artist_content.py` (edit — accept ContentValidationCache + FetchSuccessStore ports; record FETCHABLE/UNFETCHABLE per source; update FetchSuccessStore)
  - `services/api/tests/unit/altune/application/discovery/test_get_artist_content.py` (edit — verify validation + fetch success recorded)
- Failing test first: `test_artist_content_fetch_records_validation_failure` — after fetch error, ContentValidationCache.record called with UNFETCHABLE
- Verify: `pytest services/api/tests/unit/altune/application/discovery/test_get_artist_content.py -v -k "validation"`

### Slice 22: Redis adapter for content validation cache
- Acceptance criterion: AC#14 (complete — storage + TTL)
- Files:
  - `services/api/src/altune/adapters/outbound/discovery/cache/content_validation_cache.py` (new — Redis per `(provider, external_id)`, 2h TTL)
  - `services/api/tests/integration/altune/adapters/outbound/discovery/test_content_validation_cache.py` (new)
- Failing test first: `test_content_validation_cached_with_2h_ttl` — store UNFETCHABLE, verify TTL is 7200s
- Verify: `pytest services/api/tests/integration/altune/adapters/outbound/discovery/test_content_validation_cache.py -v`

### Slice 23: Quality gate — filter all-source-failed results from search
- Acceptance criterion: AC#11
- Files:
  - `services/api/src/altune/application/discovery/search_music.py` (edit — SearchMusic receives ContentValidationCache port; after fuse_and_rank, check each result's sources; filter if ALL have cached UNFETCHABLE; DI wiring note: platform/wiring.py must inject ContentValidationCache)
  - `services/api/tests/unit/altune/application/discovery/test_search_music.py` (edit — provide mock ContentValidationCache)
- Failing test first: `test_result_with_all_sources_unfetchable_filtered` — result whose every source has cached UNFETCHABLE is removed from search output
- Verify: `pytest services/api/tests/unit/altune/application/discovery/test_search_music.py -v -k "unfetchable"`

### Slice 24: Self-healing — score recovery on successful refetch
- Acceptance criterion: AC#12, AC#13
- Files:
  - `services/api/tests/unit/altune/application/discovery/test_get_album_tracks.py` (edit — integration test: record failures → score drops → record success → score recovers)
- Failing test first: `test_self_healing_score_recovers_after_success` — FetchSuccessStore rate goes from 0.4 → 0.5 after recording a success
- Verify: `pytest services/api/tests/unit/altune/application/discovery/test_get_album_tracks.py -v -k "self_healing"`
- Note: the actual recording logic was wired in slices 20-21; this slice verifies the end-to-end self-healing behavior

## Phase 4: Bug Fixes

### Slice 25: Album name stabilization determinism
- Acceptance criterion: AC#19
- Files:
  - `services/api/src/altune/application/discovery/dedup.py` (edit — sort `per_provider` groups by provider name before pre-merge `_album_best` loop)
  - `services/api/tests/unit/altune/application/discovery/test_fuse_and_rank.py` (edit)
- Failing test first: `test_album_name_deterministic_regardless_of_provider_order` — call fuse_and_rank with providers in two different orderings, assert same album name
- Verify: `pytest services/api/tests/unit/altune/application/discovery/test_fuse_and_rank.py -v -k "deterministic"`

### Slice 26: Single-char query match gate fix
- Acceptance criterion: AC#20
- Files:
  - `services/api/src/altune/application/discovery/dedup.py` (edit — when all query tokens are single chars after normalization, use the original query as a single token instead of dropping everything)
  - `services/api/tests/unit/altune/application/discovery/test_fuse_and_rank.py` (edit)
- Failing test first: `test_single_char_query_does_not_match_everything` — query "X" does not gate in "Bohemian Rhapsody"
- Verify: `pytest services/api/tests/unit/altune/application/discovery/test_fuse_and_rank.py -v -k "single_char"`

### Slice 27: RRF signal preservation in rerank
- Acceptance criterion: AC#21
- Files:
  - `services/api/src/altune/application/discovery/dedup.py` (edit — store RRF score in `extras["_rrf"]` during `fuse_and_rank` so it survives into `rerank`; `rerank()` sort key reads `extras["_rrf"]` as tiebreak)
  - `services/api/tests/unit/altune/application/discovery/test_fuse_and_rank.py` (edit)
- Failing test first: `test_rerank_preserves_rrf_signal` — result with high RRF and low popularity outranks result with low RRF and high popularity (when other signals equal)
- Verify: `pytest services/api/tests/unit/altune/application/discovery/test_fuse_and_rank.py -v -k "rrf_rerank"`
- Note: RRF currently lives on the `_Scored` wrapper, not on `SearchResult`. Storing it in extras bridges the gap without changing `SearchResult`'s frozen interface.

### Slice 28: Backend album dedup source preservation
- Acceptance criterion: AC#22
- Files:
  - `services/api/src/altune/application/discovery/get_artist_content.py` (edit — `_dedup_albums` merges sources from both winner and loser, deduped by `(provider, external_id)`)
  - `services/api/tests/unit/altune/application/discovery/test_get_artist_content.py` (edit)
- Failing test first: `test_album_dedup_preserves_sources_from_all_providers` — two albums with different sources merge, both sources present in result
- Verify: `pytest services/api/tests/unit/altune/application/discovery/test_get_artist_content.py -v -k "source_preservation"`

### Slice 29: Cache write completeness validation
- Acceptance criterion: AC#23
- Files:
  - `services/api/src/altune/application/discovery/search_music.py` (edit — `_call_provider_with_cache` skips cache write when response has error/timeout status AND fewer items than limit)
  - `services/api/tests/unit/altune/application/discovery/test_search_music.py` (edit)
- Failing test first: `test_partial_error_response_not_cached` — provider returns 3 items with timeout status when limit was 25, cache.set is NOT called
- Verify: `pytest services/api/tests/unit/altune/application/discovery/test_search_music.py -v`

### Slice 30: SoundCloud album items null safety
- Acceptance criterion: AC#24
- Files:
  - `apps/mobile/src/features/detail/hooks/useArtistContent.ts` (edit — guard `scAlbumsQuery.data?.items` with explicit null/undefined fallback to `[]`)
  - `apps/mobile/src/features/detail/__tests__/useArtistContent.test.ts` (edit or new)
- Failing test first: `test_soundcloud_null_items_no_crash` — scAlbumsQuery.data is `{ items: null }`, no runtime error
- Verify: `cd apps/mobile && npx jest --testPathPattern="useArtistContent" --verbose`

### Slice 31: Frontend normalization — shared utility with parity tests
- Acceptance criterion: AC#18 (partial — build the utility)
- Files:
  - `apps/mobile/src/shared/lib/normalize-for-dedup.ts` (new — full 8-step port of backend `normalize_for_match`: NFKC, lowercase, diacritic strip, bracket removal, feature-token normalization, leading article strip, punctuation normalize, whitespace collapse)
  - `apps/mobile/src/shared/lib/__tests__/normalize-for-dedup.test.ts` (new — parity tests against known backend outputs for each of the 8 steps)
- Failing test first: `test_normalize_strips_diacritics` — `normalizeForDedup("Café Del Mar")` returns `"cafe del mar"`
- Verify: `cd apps/mobile && npx jest --testPathPattern="normalize-for-dedup" --verbose`

### Slice 32: Frontend normalization — swap import in useArtistContent
- Acceptance criterion: AC#18 (complete — wire the utility)
- Files:
  - `apps/mobile/src/features/detail/hooks/useArtistContent.ts` (edit — remove inline `normalizeForDedup` function, import from `shared/lib/normalize-for-dedup`)
- Failing test first: `test_album_dedup_uses_shared_normalizer` — existing useArtistContent tests pass with the new import (this is a regression guard, not a new behavior)
- Verify: `cd apps/mobile && npx jest --testPathPattern="useArtistContent" --verbose`

## Risks

- **MBID coverage gap** (from spec): underground/unreleased tracks may lack MBIDs. The fallback chain (slices 8-9) must handle NONE tier gracefully — quality scorer assigns 0.2 (not 0) so these results still appear, just ranked lower.
- **MusicBrainz rate limit** (from spec): 1 req/s. MBID resolution in slices 5-9 operates on already-merged results (not per-provider-result), so volume is bounded by the result set size (~50 max). Existing adapter caching further reduces live calls.
- **Chain of Responsibility anti-pattern** [vault: wiki/concepts/Chain of Responsibility Pattern.md]: "no guarantee a request is handled." Mitigated: our pipeline chain is impure (all gates run), and the only hard filter is AC#11 (all-source-failed). No result silently falls off.
- **Strategy pattern over-engineering** [vault: wiki/concepts/Strategy Pattern.md]: "added number of objects — each strategy is a class even if the algorithm is trivial." The resolution tiers are an enum + branching in `_try_merge`, not a class-per-strategy hierarchy. Python functional style avoids the bloat.
- **Golden case expansion effort** (slices 1-2): creating 40+ curated cases from live responses is labor-intensive. Mitigate by using the existing `ProviderHit` schema and focusing on ranking properties (relative ordering) not exact content.
- **Sort key change regression risk** (slices 15-18): each sort-key-affecting slice must be followed by an eval harness run to catch MRR regressions immediately. The eval harness baseline from Phase 0 is the regression guard.
- **Static constant removal** (slice 18) builds on slice 17 which provides signal-based replacements. Run full test suite after slice 18 to confirm no breakage.
- **`_try_merge` return type change** (slice 5): changes propagate to `fuse_and_rank`'s merge loop. Slice 5 is structured as the return-type refactor first, before behavior changes in slices 6-9.
- **DI wiring**: slices 16, 23 inject new ports into SearchMusic; slices 20-21 inject new ports into content-fetch use cases. Each mentions the wiring concern. `platform/wiring.py` must be updated in each slice that adds a port dependency.

## ADR candidates

- None identified. This rework operates within the boundaries of ADR-0007 (scatter-gather, provider set, circuit breaker). The new domain types (EntityResolutionTier, QualityScore, ContentValidationStatus) are internal to the discovery bounded context and don't warrant an architectural decision record.
