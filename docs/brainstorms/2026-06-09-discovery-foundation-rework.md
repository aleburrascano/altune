---
date: 2026-06-09
status: active
graduates-to: docs/specs/discovery-foundation-v1/spec.md
related:
  - docs/adr/0007-unified-music-search.md
  - docs/specs/discover-music-v1/spec.md
  - docs/specs/discover-music-v2/spec.md
  - docs/specs/discover-music-v3/spec.md
  - docs/specs/discover-music-v4/spec.md
  - docs/specs/discovery-quality-v1/spec.md
  - docs/solutions/2026-06-07-extras-merge-provider-priority.md
  - "[vault: wiki/concepts/Anti-Corruption Layer Pattern.md]"
  - "[vault: wiki/concepts/Repository Pattern.md]"
---

# Brainstorm — Discovery foundation rework

## 1. Frame

The discovery pipeline (search → dedup → rank → enrich → display) evolved through 5 specs and works for the common case. Daily use reveals three structural problems that incremental fixes can't address:

1. **Artist disambiguation** — short/common names (e.g., "Che") blend albums from different artists. Identity is name-similarity-based (JW ≥ 0.92) with MBID as a secondary signal, not as the spine.
2. **Ghost results** — albums appear that return "couldn't load tracks" when tapped. No content validation or post-failure eviction.
3. **Hardcoded fragility** — junk-title keywords, static provider weights, dedup thresholds, stopwords. Each requires manual patching; none derive from observable signals.

A code audit surfaced 9 additional bugs: frontend normalization mismatch, album name race conditions, MBID merges without title validation, popularity enrichment only hitting top 25, single-char query gate bypass, backend album dedup losing sources, demotion sub-band edge cases, cache completeness gaps, SoundCloud null safety.

**Design philosophy (user-stated):** No config-driven knob-twiddling. The system should derive quality from observable signals and be validated by an eval harness — not from manually tuned constants that "feel right."

## 2. Decisions

### D1 — Approach: hybrid three-layer + eval harness

Considered:
- **Signal-based pipeline only** — keep architecture, replace hardcoded values with computed signals. Rejected alone: doesn't fix structural identity problems.
- **Entity-centric rebuild only** — MBID as identity spine. Rejected alone: incomplete MB coverage for underground content.
- **Quality gates only** — validate before display. Rejected alone: treats symptoms, not root cause.
- **Hybrid of all three + eval harness** — ✅ Selected. Entity resolution fixes identity, signal scoring fixes ranking, quality gates fix display, eval harness validates everything.

### D2 — Bootstrap strategy (pre-launch, no user data)

Eval harness (golden query set, MRR@10/nDCG@10) as ground truth + provider metadata signals (completeness, agreement, fetch success) replacing static weights. Learning-to-rank deferred until click data exists at volume.

### D3 — Artist disambiguation UX

Best match first + "Other artists named X" surfaced below. Distinct MBIDs = distinct artists.

### D4 — Sequencing: foundation before UX

Fix the algorithmic core (entity resolution, scoring, validation) before touching UX (disambiguation cards, artist images, "did you mean"). UX improvements become the next spec.

### D5 — Content validation pattern

Lazy validation with cached failure signals + self-healing. No pre-validation per result (industry consensus: no major platform does this; untenable at query latency budgets). Failed fetches update signal scores → future queries demote.

## 3. Requirements

### Layer 1 — Entity Resolution (MBID as identity spine)

- Resolve every search result to a MusicBrainz entity where possible
- Different MBIDs = different artists/recordings/releases
- Fallback chain: MBID → ISRC → duration+title fuzzy (15s window + artist match)
- Artist disambiguation: most relevant first, others surfaced as alternatives

### Layer 2 — Signal-Based Quality Scoring

- Composite quality scores from observable signals:
  - Metadata completeness (ISRC, image, duration, album)
  - Multi-source agreement (N providers)
  - Entity resolution confidence (MBID vs ISRC vs fuzzy)
  - Historical fetch success rate per provider
- Metadata anomaly patterns replace hardcoded junk-title keywords
- Entity-resolution-tier-based merge decisions replace static JW thresholds

### Layer 3 — Quality Gates with Self-Healing

- Content gate: fetchable from ≥1 source? (lazy + cached)
- Entity gate: distinct real entity? (MBID-backed or multi-source)
- Quality gate: composite signal score meets minimum
- Self-healing: failed fetches update signal scores for future queries

### Eval Harness (cross-cutting)

- Golden query set (50-200 queries): happy path, partial/misspelled, ambiguous, nonsense, cover traps
- MRR@10 and nDCG@10 per change
- Regression detection: no change may regress the golden set
- Shadow evaluation: old vs new ranker comparison

### Bug Fixes

1. Frontend normalization mismatch (silent duplicate albums)
2. Album name race condition (non-deterministic provider ordering)
3. MBID merge without title validation
4. Popularity enrichment lag (top 25 only, RRF dropped in rerank)
5. Single-char query gate bypass
6. Backend album dedup loses sources
7. Demotion sub-band edge case
8. Cache writes don't validate completeness
9. SoundCloud album items null safety

## 4. Out of scope

- Learning-to-rank ML models
- Audio fingerprinting / AcoustID
- New provider integrations
- UX-layer improvements (disambiguation UI, "did you mean", search suggestions, artist images)
- Autocomplete / search suggestions
- Pagination / infinite scroll
- Playback integration

## 5. Research grounding

- **Artist disambiguation**: MusicBrainz uses MBID + human-curated disambiguation comments. Deezer Research (ISMIR 2018) published audio-embedding approach. No platform has fully automated it.
- **Content validation**: No major platform pre-validates. Universal pattern is lazy validation at fetch time. Music Assistant (open-source) validates at playback, not at index.
- **Entity resolution**: Navidrome uses 4-tier pipeline (external_id → MBID → ISRC → fuzzy). Duration matching ≈ 90% precision as secondary signal.
- **Ranking without hardcoded weights**: RRF is the standard no-training approach. XGBoost LambdaMART is the next step with click data.
- **Eval-driven quality**: 50-200 golden queries + MRR/nDCG is the solo-dev A/B testing equivalent.
- **Quality scoring**: Composite signals (completeness + freshness + engagement) replace static weights. CRM data quality literature provides the formula pattern.

## 6. Risks

- **MBID coverage gap**: Underground/unreleased tracks won't have MBIDs. Fallback chain must be robust.
- **MusicBrainz rate limits**: 1 req/s without auth. Aggressive caching needed for per-result resolution.
- **Over-filtering**: Quality gates could suppress valid niche content with incomplete metadata.
- **Eval harness maintenance**: Golden query set needs periodic refresh as providers change.

## 7. Next

Graduates to `docs/specs/discovery-foundation-v1/spec.md` via `/feature-spec`.
