---
date: 2026-06-18
topic: discovery-pipeline-hardening
---

# Discovery Pipeline Hardening

## Summary

Fix the two root causes of incorrect search results (aggressive artist merge, vocabulary pollution) and clean up 15 additional code quality issues surfaced by a full audit of `services/go-api/internal/discovery/`.

---

## Problem Frame

The discovery pipeline has grown to 14 stages over many sessions. It works well for mainstream queries but breaks for short/ambiguous artist names: searching "Che" can show the wrong artist's profile picture and discography because two unrelated artists with the same name get merged into one result. The merged result's Deezer external ID may belong to the wrong "Che", so tapping into the detail screen fetches the wrong artist's content.

A second issue compounds this: every search ingests its top 5 results into the vocabulary store with no quality gate. If a wrong result ranks in the top 5, it enters the vocabulary and reinforces itself in future searches via intent detection, correction, and suggestions. Pre-correction was already disabled because of this feedback loop.

Beyond these two root causes, a full code audit found 15 additional issues across the pipeline: dead code, inconsistent sort keys, enum zero-value risks, swallowed errors, duplicated functions, and minor correctness bugs.

---

## Requirements

**Artist merge integrity**
- R1. Artists must only merge when they share a MusicBrainz ID (MBID). The current name-only merge for artists without MBID must be removed entirely.
- R2. A regression test must verify that two artists with the same normalized name but different providers and no MBID remain as separate results after FuseAndRank.
- R3. MusicBrainz artist results must include the `disambiguation` field in extras when the API returns one.

**Vocabulary quality gate**
- R4. Vocabulary ingestion must skip results with popularity < 30.
- R5. The error from `vocabStore.Add` must not be discarded — log failures at warn level.

**Ranking consistency**
- R6. Rerank (post-enrichment re-sort) must include quality score in the sort key, matching the initial rankingKeyLess, OR carry an explicit code comment documenting the deliberate exclusion and why.
- R7. The PopularityDominance condition must be rewritten using named boolean variables for readability.

**Dead code removal**
- R8. Remove `ApplyIntentBoost` from intent.go (dead — FuseAndRank has inline intent boost).
- R9. Remove `dedupRelatedGroups` from find_related.go (defined, never called).
- R10. Remove `preQueryCorrection` from search_music.go (dead — Execute uses tryCorrection). Update any test that references it.

**Enum safety**
- R11. Add `ResultKindUnknown` at iota 0 in the ResultKind enum. Shift existing members.
- R12. Add `ProviderUnknown` at iota 0 in the ProviderName enum. Shift existing members.

**Error visibility**
- R13. Provider `Search()` methods must log per-kind errors instead of silently continuing.

**Code consistency**
- R14. `maxLevenshtein` in vocabulary_store.go must operate on rune count, matching correction.go.
- R15. `levenshteinDistance` in fuzzy.go must operate on runes, not bytes.
- R16. `attachScores` in vocabulary_store.go must receive context from the caller instead of creating context.Background().
- R17. `SetNoisePatterns` global mutable state must be eliminated (make CleanQuery accept patterns or make the var immutable after init).

**Minor correctness**
- R18. `boostIfRecent` must preserve float64 precision instead of truncating to int64.
- R19. `EnforceDiversity` guard must use `<` instead of `<=` so the window boundary is enforced.
- R20. `VocabularyEntry.Kind` should be a named string type for type safety.

---

## Acceptance Examples

- AE1. **Covers R1, R2.** Given two artist results named "Che" from Deezer and Last.fm respectively, both without MBID, when FuseAndRank processes them, they remain as two separate results in the output — not merged.
- AE2. **Covers R4.** Given a search result with popularity 15 in the top 5, when vocabulary ingestion runs, that result is not added to the vocabulary store.
- AE3. **Covers R11.** Given an uninitialized `ResultKind` variable (zero value), its String() returns "unknown", not "artist".

---

## Success Criteria

- All existing canonical ranking tests pass (`TestCanonicalRanking`) with no regressions.
- All existing regression tests pass, including updated ones that referenced dead code.
- New regression test for same-name artist collision passes.
- `go vet ./internal/discovery/...` passes clean.
- No `context.Background()` calls in non-root code paths within the discovery package.

---

## Scope Boundaries

- Stage-removal experiment (evaluating which of 14 stages earn their keep)
- Moving hardcoded constants to runtime config
- ML-based ranking or embedding-based disambiguation
- Click-confirmed vocabulary ingestion
- Adding genre field to SearchResult type

---

## Key Decisions

- **Never merge artists without identifiers** rather than adding heuristic guards (fan-count, genre). Rationale: PopularityDominance and EnforceDiversity already handle the "too many copies" problem — adding merge heuristics adds complexity that can silently break.
- **Popularity >= 30 floor for vocab ingestion** rather than click-confirmed. Rationale: click-confirmed delays vocab population for new artists; popularity floor is simpler and blocks the main pollution vector (low-quality/ambiguous results).
- **Fix all 17 findings** in one pass rather than spreading across sessions. Rationale: most fixes are surgical (1-5 lines), the audit context is fresh, and batching avoids re-reading the same files.
