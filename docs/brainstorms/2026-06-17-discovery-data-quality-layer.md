---
date: 2026-06-17
topic: discovery-data-quality-layer
---

# Discovery Data Quality Layer

## Summary

Harden the discovery pipeline's data quality by fixing cross-provider ISRC merge (MusicBrainz), adopting provider-native popularity as the sole popularity source, and adding adaptive popularity banding at the top of the scale. Eliminates the enrichment contamination class entirely and improves merge reliability for mega-hits.

---

## Problem Frame

The ranking algorithm is solid — 96.3% on library queries (78/81), 86% on broad queries (43/50), 100% on canonical queries (9/9). Three ranking improvements shipped this session (popularity banding, max enrichment, artist source bonus) with 14 regression tests locking them in. 221 tests pass.

The remaining failures are not algorithm problems. They trace to upstream data quality:

- **Enrichment contamination**: the popularity resolver does a title+artist lookup without entity-type context, so artist "StarBoy" absorbs track "Starboy"'s popularity from The Weeknd. The `PopularityResolver` port accepts `(title, artist string)` only — `result.Kind` is available at the call site but never passed.
- **ISRC merge gaps**: MusicBrainz queries omit the `inc=isrcs` parameter, so MusicBrainz never returns ISRC data. Deezer is the only ISRC-bearing source, making cross-provider ISRC merge effectively Deezer-to-Deezer only. This means popular tracks that should consolidate across providers (e.g., Ed Sheeran's "Shape of You") sometimes surface as separate results with lower individual popularity signals.
- **Top-of-scale banding ties**: 5-point popularity bands work well generally but create ties among mega-hits (pop 95-99 all floor to 95), letting multi-source covers tie or beat single-source originals.

The popularity resolver is not wired in production. Provider-reported metrics (Deezer rank, nb_fan, Last.fm listeners, SoundCloud playback_count) already compute popularity via `NormalizePopularity()` without it. Fixing ISRC merge so popular tracks consolidate multi-source signals naturally, combined with adaptive banding, addresses the remaining failures without adding speculative enrichment infrastructure.

---

## Requirements

**Cross-provider identity merge**

- R1. MusicBrainz recording queries must request ISRC data so that tracks returned from MusicBrainz can merge with tracks from other providers via ISRC match.
- R2. The existing ISRC merge logic in the dedup pipeline must work unchanged once MusicBrainz provides ISRCs — no new merge paths or fuzzy matching.

**Provider-native popularity**

- R3. The popularity resolver must not be wired in the production application. Provider-reported metrics (Deezer rank, nb_fan, listeners, playback_count) remain the sole popularity source via `NormalizePopularity()`.
- R4. The existing `enrichOne()` max-enrichment logic (`maxI64(resolved, existing)`) is retained but only fires if a resolver is explicitly configured (test environments, future entity-aware resolver).

**Adaptive popularity banding**

- R5. Popularity banding must use narrower bands at the top of the scale to prevent ties among mega-hits. High-popularity results must be distinguishable from each other rather than collapsing into the same band.
- R6. Below the adaptive threshold, the existing 5-point banding behavior is preserved.

---

## Acceptance Examples

- AE1. **Covers R1, R2.** Given a search for "Shape of You", when Deezer returns Ed Sheeran's track with ISRC GBAYE7500101 and MusicBrainz returns a recording with the same ISRC, the two results merge into one multi-source result with consolidated popularity.
- AE2. **Covers R5, R6.** Given two tracks with popularity 99 and 95, banding must place them in different bands. Two tracks with popularity 72 and 74 may share the same band.
- AE3. **Covers R3.** Given the production application startup, no `PopularityResolver` is injected into the discovery service. Enrichment runs artwork resolution only.

---

## Success Criteria

- Broad query pass rate improves from 86% (43/50) — StarBoy and Shape of You resolve correctly.
- Library query pass rate maintains at or above 96.3% (78/81).
- All 14 existing regression tests continue to pass; new tests cover MB ISRC merge and adaptive banding.
- No new external API calls introduced in the enrichment pipeline.

---

## Scope Boundaries

- Wiring the popularity enrichment resolver in production (deferred until a future spec needs entity-aware enrichment with identifier-based lookup)
- Adding new search providers to fix coverage gaps (AURORA not appearing is a Deezer limitation, not a ranking pipeline concern)
- Fuzzy/text-similarity merge for tracks (deliberately removed per identifier-only-merge brainstorm)
- Test harness normalization (JID spelled "J.I.D" by Deezer is a checker issue, not a pipeline issue)
- Modifying the ranking key order (relevance_band → demoted → bandPop → multi_source → quality → RRF → subtitle → title)

---

## Key Decisions

- **Provider-native over speculative enrichment**: Rather than making the popularity resolver entity-aware (kind-gated or identity-first), we eliminate the contamination class by not wiring speculative enrichment at all. Provider-reported metrics are sufficient when ISRC merge works correctly. The resolver code stays in the codebase for potential future use but is not production-active.
- **Adaptive banding, not removal**: Banding remains valuable for suppressing 1-2 point noise in the mid-range. Only the top of the scale gets narrower bands to preserve distinguishability among mega-hits.

---

## Dependencies / Assumptions

- MusicBrainz API supports `inc=isrcs` on recording search queries (documented in MB API v2 spec).
- The existing `tryMerge()` ISRC path works correctly when both sides have ISRCs — the gap is data availability, not logic.
- Deezer continues to be the primary ISRC-bearing provider with ~85-95% coverage for tracks.

---

## Outstanding Questions

### Deferred to Planning

- [Affects R5][Technical] What adaptive banding curve best separates mega-hits? Options include 3-point bands above 90, 2-point bands above 95, or a logarithmic narrowing curve.
- [Affects R1][Needs research] Does the MusicBrainz `inc=isrcs` parameter work on search endpoints or only on direct recording lookups? If search-only returns IDs, a follow-up lookup per recording may be needed.
