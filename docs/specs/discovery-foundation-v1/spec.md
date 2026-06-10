# Discovery Foundation Rework

> Spec for `discovery-foundation-v1` — version 1, drafted 2026-06-09.
> Authors: solo + Claude.
> Status: Draft.

## Problem

The discovery pipeline (search → dedup → rank → enrich → display) blends different artists who share the same name into one discography (e.g., searching "Che" mixes albums from unrelated artists across decades), shows albums that return "couldn't load tracks" when tapped (e.g., some versions of "16*29"), and relies on hardcoded keyword lists, static provider weights, and magic thresholds that break on every new edge case. A code audit surfaced 9 additional bugs including non-deterministic album names, MBID merges without title validation, and frontend normalization mismatches causing silent duplicate albums.

## User value

Every search result is a real, distinct entity that delivers content when tapped. Artists with the same name are separated, not blended. Ranking quality improves automatically as the system observes provider behavior — no manual keyword lists or weight tweaking. An eval harness catches regressions before they ship.

## Scope tier / MVP cut

- **Minimal (ship this):**
  - Eval harness with golden query set and MRR@10 measurement
  - MBID-based entity resolution with fallback chain
  - Signal-based quality scoring replacing all static weights/thresholds/keyword lists
  - Quality gates filtering unfetchable content with self-healing
  - 7 bug fixes from code audit (2 deferred: top-25 enrichment limit is a performance constraint not a quality bug; demotion sub-band edge is low severity and already mitigated)
- **Deferred to post-launch:**
  - Learning-to-rank ML models (needs click data volume)
  - Audio fingerprinting / AcoustID
  - New provider integrations (Discogs, etc.)
- **Justified exceptions:** All four layers are needed now because the existing system has user-visible quality problems encountered daily — broken artist identity, ghost results, and hardcoded fragility requiring manual patches. This is reworking existing shipped infra, not adding speculative scale.

The Acceptance criteria below cover the **minimal tier only**.

## Acceptance criteria

### Entity resolution

1. **AC#1** — Given two search results with matching MBIDs and normalized title similarity ≥ 0.85, when fuse_and_rank merges them, then they produce one merged result at the MBID resolution tier.
2. **AC#2** — Given two search results with different MBIDs but identical normalized names (e.g., two artists named "Che"), when fuse_and_rank runs, then they remain as separate results.
3. **AC#3** — Given a result with an MBID and a result without one, when both match by ISRC, then they merge using the ISRC resolution tier (not rejected for MBID absence).
4. **AC#4** — Given two results that share no MBID and no ISRC, when their titles match by fuzzy similarity AND both have `extras["duration_seconds"]` within 15 seconds of each other, then they merge at the duration-confirmed tier. When one or both results have no duration in extras, the duration check is skipped and resolution falls through to the name-only fuzzy tier.
5. **AC#5** — Given two results that share no MBID, no ISRC, and have durations differing by >15 seconds (both durations present), when their titles match by fuzzy similarity, then they remain separate (duration mismatch blocks merge).
6. **AC#6** — Given two results with matching MBIDs but normalized title similarity < 0.85, when fuse_and_rank evaluates them, then the merge is rejected (MBID alone is insufficient without title agreement — prevents false merges of different recordings grouped under one MB entity).

### Signal-based quality scoring

7. **AC#7** — Given the codebase after this spec ships, when grepping for the removed static provider priors (`_PRIORS` dict), junk-title markers (`_JUNK_TITLE_MARKERS`), and stopword set (`_STOPWORDS`), then none exist as module-level constants.
8. **AC#8** — Given a search result, when the quality scorer evaluates it, then the score is a composite of: metadata completeness (has ISRC, image, duration, album), multi-source agreement count, entity resolution tier (MBID > ISRC > duration-confirmed > fuzzy-only), and historical fetch success rate. Fetch success history is stored per `(provider, external_id)` in Redis with a sliding window of the last 10 fetch attempts and a 48-hour TTL. Starting formula: equal-weight additive across the four signals, normalized to [0, 1]. The formula is adjustable via eval harness tuning.
9. **AC#9** — Given a result with no ISRC, no image, from a single source, whose title embeds the full search query, when scoring runs, then it scores lower than a multi-source result with ISRC and image for the same query.
10. **AC#10** — Given the ranking sort key, when sorting results, then quality score replaces the former `_PRIORS`-based prior AND subsumes the `multi_source` signal (since multi-source agreement is a component of the quality score). Position in sort key: `(-band, demoted, bootleg, -quality_score, -popularity, -rrf, subtitle, title)`.

### Quality gates

Content validation is **synchronous at content-fetch time** (when user taps into album/artist detail), not at search time. Results appear in search; validation fires when the user drills in. Failures are cached and fed back into signal scores for future search queries. This matches the industry-standard "validate at access time" pattern (Music Assistant, Spotify, Apple Music all validate at playback, not at index).

A **content-fetch error** is defined as: HTTP 4xx (except 429 rate-limit), HTTP 5xx, timeout after the per-source timeout budget, or a successful HTTP response with an empty items array. Rate-limited (429) and circuit-open statuses do NOT count as unfetchable (provider may recover). A response with at least one track/item is considered fetchable.

11. **AC#11** — Given a search result whose every source has a cached content-fetch failure (no source returned items on last attempt), when the quality gate evaluates it in a subsequent search, then the result is filtered from display.
12. **AC#12** — Given a result that was previously fetchable but whose content fetch now fails, when the failure is recorded, then the fetch success rate signal decreases and the result is demoted in future search queries until the cached failure TTL expires.
13. **AC#13** — Given a result whose cached failure TTL has expired, when a user taps into it and content fetch succeeds, then the fetch success rate signal is recalculated from current signals (not restored to a prior value) and the result reappears at appropriate rank in future queries.
14. **AC#14** — Given a content validation result (success or failure), when stored, then it is cached in Redis per `(provider, external_id)` with a 2-hour TTL and revalidated on expiry — not on every content fetch.

### Eval harness

Golden queries are hand-curated from live provider responses during Phase 0. Each golden case uses binary relevance (expected title + subtitle match, same as the existing eval harness schema). The existing 10 golden cases are preserved and expanded to ≥50.

15. **AC#15** — Given the eval harness, when listing golden queries, then there are ≥50 queries covering: popular artists/albums, partial/misspelled queries, ambiguous short-name queries (e.g., "Che", "X"), nonsense queries (expected: zero results), and cover/karaoke trap queries (expected: genuine above cover).
16. **AC#16** — Given a golden query with annotated expected results, when the current ranker processes it, then MRR@10 and hit@3 are computed and reported (matching the existing eval harness metrics).
17. **AC#17** — Given the baseline MRR@10 scores recorded before this rework, when any subsequent ranking change is evaluated, then aggregate MRR@10 must not regress below baseline. This is enforced as a pytest assertion in CI.

### Bug fixes

18. **AC#18** — Given the frontend `normalizeForDedup()` function, when normalizing a string with diacritics, feature tokens (feat./ft./featuring), or leading articles (the/los/les), then the output matches the backend `normalize_for_match()` output for the same input. This is a full 8-step port, reversing the `discovery-quality-v1` decision to use a 4-step shortcut — the brainstorm surfaced that the shortcut causes real dedup failures with diacritics and feat. variants.
19. **AC#19** — Given scatter-gather results arriving in any provider order, when album name stabilization runs, then the chosen album name is deterministic (same query → same album name, regardless of response timing).
20. **AC#20** — Given a search for "X" (single character), when the match gate evaluates results, then it does not pass all results through — single-char tokens are handled, not dropped.
21. **AC#21** — Given the rerank step after popularity enrichment, when sorting, then the RRF signal is preserved from the initial fuse_and_rank (not silently dropped).
22. **AC#22** — Given two duplicate albums from different providers merged in the backend, when the merge completes, then both providers' source links are preserved in the merged result.
23. **AC#23** — Given a provider response that returns fewer items than the requested `limit` AND the provider returned an error status or timeout, when the cache-write step evaluates it, then it is not cached as canonical (either skipped or marked as partial). A response with fewer items than `limit` but a successful status (provider simply has fewer results) IS cached normally.
24. **AC#24** — Given SoundCloud album data with null or malformed `items`, when `useArtistContent` processes it, then no runtime error occurs and the null case is handled gracefully.

## Out of scope

- Learning-to-rank ML models (XGBoost LambdaMART) — needs click data at volume; deferred until post-launch traffic exists.
- Audio fingerprinting / AcoustID integration — heavy infrastructure dependency not justified pre-launch.
- New provider integrations (Discogs, Spotify, etc.).
- UX disambiguation UI (artist cards with "Other artists named X") — next spec after foundation.
- Artist profile images in search results — next spec.
- "Did you mean" / search suggestions / autocomplete.
- Pagination / infinite scroll.
- Playback integration from detail screen.
- Demotion sub-band edge case (bug #7 from audit) — low severity, mitigated by content-token scoring already in place.

## Design considerations

- [vault: wiki/concepts/Chain of Responsibility Pattern.md] — the quality gates layer is an impure/pipeline chain: every result passes through entity gate → content gate → quality gate in sequence. Each gate enriches the result with its verdict (score contribution) rather than short-circuiting. This keeps gates composable and independently testable.
- [vault: wiki/concepts/Strategy Pattern.md] — entity resolution uses the Strategy pattern for the fallback chain: MBID resolution, ISRC matching, duration-confirmed fuzzy, and name-only fuzzy are interchangeable resolution strategies tried in priority order. The resolver holds a reference to the strategy chain and delegates to the first strategy that produces a match. New resolution strategies (e.g., future audio fingerprinting) plug in without modifying existing ones.

High-level approach:

- This is a **mixed read/write** rework in the `discovery` bounded context — reworking the application-layer merge/rank/score logic and adding validation state.
- It **does** require new domain concepts that must be added to `docs/ubiquitous-language.md` before implementation:
  - **EntityResolutionTier** — enum replacing the merge-time role of `Confidence`: `mbid`, `isrc`, `duration_confirmed`, `fuzzy`, `none`. Drives merge decisions and feeds into quality scoring. The existing `Confidence` enum is preserved on the wire for backward compatibility but is now derived from entity resolution tier (mbid/isrc → HIGH, duration_confirmed → MEDIUM, fuzzy → LOW, none → LOW).
  - **QualityScore** — composite [0, 1] signal score per result, computed from metadata completeness + multi-source agreement + entity resolution tier + historical fetch success rate.
  - **ContentValidationStatus** — per `(provider, external_id)` cached result: `fetchable`, `unfetchable`, `unknown`. TTL-based revalidation.
- It **does not** introduce a new external dependency — MusicBrainz is already integrated; enhanced MBID resolution queries use the existing adapter with a dedicated rate-limited queue (1 req/s) separate from the scatter-gather parallelism.
- The eval harness is test infrastructure, not production code — it lives alongside tests but is not deployed. Regression detection is a pytest assertion in CI.
- Quality gates are an impure/pipeline chain [vault: wiki/concepts/Chain of Responsibility Pattern.md] — all gates run on every result, enriching it with score contributions. There is no hard minimum composite score threshold; low-scoring results are demoted in ranking, not rejected. The only hard filter is AC#11: results with ALL sources failing content fetch are removed.

## Dependencies

- **Bounded contexts**: `discovery` (existing — all changes within this context)
- **Other features**: `discover-music-v1` through `discovery-quality-v1` (already shipped/in-progress — this reworks their internals)
- **External services**: MusicBrainz API (existing adapter — enhanced MBID resolution queries, subject to 1 req/s rate limit; mitigated by aggressive caching)
- **Library/framework additions**: none anticipated

## Risks / open questions

- **Risk**: MBID coverage gap for underground/unreleased tracks — MusicBrainz won't have entities for leaked or self-released content. Mitigation: the fallback chain (ISRC → duration+title → name-only fuzzy) must be robust; quality gates must not over-filter results that lack MBIDs but are otherwise valid.
- **Risk**: MusicBrainz rate limits (1 req/s without auth) make per-result MBID resolution expensive at query time. Mitigation: cache resolved MBIDs aggressively; resolve in the enrichment phase (top-N only, same as current popularity enrichment), not in the scatter-gather phase.
- **Risk**: Quality gates filtering valid niche content with sparse metadata (no image, no ISRC, single source). Mitigation: the composite score weights multi-source agreement and fetch success highly but doesn't hard-reject on any single missing field. Eval harness includes niche/underground queries to catch over-filtering.
- **Risk**: Eval harness golden set becomes stale as providers change catalogs. Mitigation: golden queries test ranking properties (artist X above cover Y, distinct artists separated) not exact result sets; provider-specific content changes don't invalidate the properties.
- **Resolved**: Composite score formula starts as equal-weight additive across four signals, normalized to [0, 1]. Adjustable via eval harness tuning during implementation.
- **Resolved**: Content validation is synchronous at content-fetch time (user taps into detail), not at search time. Matches industry pattern. See AC#11-14 preamble.

## Telemetry

- **Log events**:
  - `entity_resolved` — per result: resolution tier used (mbid/isrc/duration/fuzzy/none), provider, confidence
  - `quality_gate_filtered` — result filtered before display: reason (unfetchable/low-score/no-entity), result signature
  - `content_validation_result` — fetch success/failure per (provider, external_id): status, latency
  - `signal_score_computed` — per result: composite score breakdown (completeness, agreement, entity_tier, fetch_success)
  - `eval_run_completed` — golden set evaluation: MRR@10, hit@3, query count, regression detected (bool)
- **Metrics**:
  - `discovery.entity_resolution_tier` — histogram of resolution tiers per query (what % resolve to MBID vs fallback?)
  - `discovery.quality_gate_filter_rate` — % of results filtered per query (is the gate too aggressive?)
  - `discovery.content_validation_hit_rate` — cache hit rate for content validation (is caching effective?)
  - `discovery.signal_score_distribution` — histogram of composite scores (are scores well-distributed or clustered?)
- **Alerts**:
  - Eval harness regression detected (MRR@10 drops >5% from baseline) — pytest assertion failure blocks CI
  - Quality gate filter rate >50% sustained — investigate over-filtering
  - Content validation cache miss rate >80% sustained — caching not effective

## Related

- [vault: wiki/concepts/Chain of Responsibility Pattern.md] — quality gates pipeline
- [vault: wiki/concepts/Strategy Pattern.md] — entity resolution fallback chain
- Related ADR: `docs/adr/0007-unified-music-search.md` (locks scatter-gather, provider set, circuit breaker decisions — this spec reworks internals within those boundaries)
- Predecessor specs: `docs/specs/discover-music-v1/spec.md`, `docs/specs/discover-music-v2/spec.md`, `docs/specs/discover-music-v3/spec.md`, `docs/specs/discover-music-v4/spec.md`, `docs/specs/discovery-quality-v1/spec.md`
- Brainstorm: `docs/brainstorms/2026-06-09-discovery-foundation-rework.md`
