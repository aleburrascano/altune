---
date: 2026-06-10
status: active
graduates-to: docs/specs/discovery-identity-v1/spec.md
related:
  - docs/brainstorms/2026-06-09-discovery-foundation-rework.md
  - docs/specs/discovery-foundation-v1/spec.md
  - docs/adr/0007-unified-music-search.md
---

# Brainstorm — Identifier-only merge (zero heuristics)

## 1. Frame

The foundation rework (discovery-foundation-v1) added entity resolution tiers,
quality scoring, and quality gates. But the merge pipeline still relies on
Jaro-Winkler similarity thresholds, keyword regex lists, stopword sets, and
static provider priors — all hardcoded constants that produce edge-case bugs
(artist "Che" discography contamination) and can't scale.

The user's core demand: **the system should derive correctness from provider
identity signals, not from manually tuned heuristics.** Every search for any
query should produce correct results without adding new constants.

## 2. Decisions

### D1 — Merge ONLY on shared identifiers, never on text similarity

Two results merge if and only if:
- They share an ISRC (recordings), or
- They share an MBID (any kind), or
- A MusicBrainz URL lookup confirms a cross-provider link
  (`/ws/2/url?resource=https://deezer.com/artist/<id>` → MBID)

No JW similarity. No duration matching. No name-based merging. If two
results have no shared identifier, they appear as separate entries. The
quality score and relevance ranking push the best one to the top.

This removes: `_JW_HIGH`, `_JW_MEDIUM`, `_JW_ARTIST`, `_DURATION_TOLERANCE_S`,
`EntityResolutionTier.FUZZY`, `EntityResolutionTier.DURATION_CONFIRMED`.

### D2 — Provider metadata for ranking, not keyword lists

Demotion (karaoke/cover/tribute) is driven by:
- Provider `record_type` metadata (Deezer, MusicBrainz, iTunes already tag these)
- Quality score (metadata completeness, source agreement, fetch success)
- Popularity signal (genuine tracks are more popular than covers)

No keyword scanning of titles. If a provider doesn't tag a karaoke track
as karaoke, we don't try to guess — the quality score naturally ranks it
lower (single source, lower popularity, no ISRC match with the genuine).

This removes: `_COVER_PHRASE_RE`, `_COVER_WORD_RE`, `_DEMOTED_TYPES`,
`_FUNCTION_WORDS`, `_SET_NOISE_RE`.

### D3 — Relevance scoring replaces the match gate

Instead of token intersection with stopword filtering, the match gate uses
`_relevance_score > 0.0` (any non-zero fuzzy text similarity). Results with
zero relevance are dropped. No stopword list needed.

This removes: `_FUNCTION_WORDS` and the `_content_tokens` / `_passes_gate`
system entirely.

### D4 — MusicBrainz URL lookup as the cross-provider bridge

New enrichment step: after search, for results that carry a provider URL
(Deezer, SoundCloud, Last.fm), query MusicBrainz's URL endpoint to resolve
to an MBID. If two results from different providers resolve to the same
MBID, merge them. This is the ONLY cross-provider merge path.

API: `GET /ws/2/url?resource=<provider_url>&inc=artist-rels&fmt=json`
Rate limit: 1 req/s (existing MB adapter caching applies).
Coverage: Good for artists/releases, sparse for recordings.

### D5 — Primary-source discography

When the user taps an artist, only fetch discography from sources on that
specific result card. No fan-out by artist name. If the card has
`sources: [deezer/234701081]`, only query Deezer. If it also has
`sources: [musicbrainz/0a68f3b5...]` (because MB URL lookup confirmed the
link), query both.

This eliminates the SoundCloud-by-name fan-out that mixed different artists.

### D6 — What stays (infrastructure constants)

These are NOT domain heuristics and stay:
- Cache TTLs (24h/12h/6h/1h) — infrastructure config
- Circuit breaker params (5 failures, 30s recovery) — resilience pattern
- Timeouts (1.5s default) — network config
- `_RRF_K = 60` — algorithm parameter from the RRF paper
- Enrichment limits/concurrency — performance tuning
- UI constants (debounce 300ms, section cap 10) — UX design
- Default page limits (5 top tracks, 50 album tracks) — data size
- `normalize.py` regexes — text canonicalization (NLP, not identity)
- API base URLs, keys — infrastructure

## 3. Constants removed (full list)

| Constant | File | Replaced by |
|----------|------|-------------|
| `_JW_HIGH = 0.92` | dedup.py | Identifier-only merge |
| `_JW_MEDIUM = 0.85` | dedup.py | Identifier-only merge |
| `_JW_ARTIST = 0.92` | dedup.py | Identifier-only merge |
| `_DURATION_TOLERANCE_S = 15.0` | dedup.py | Identifier-only merge |
| `_fallback` provider priors | dedup.py | Quality score |
| `_COVER_PHRASE_RE` | quality_scorer.py | Provider record_type metadata |
| `_COVER_WORD_RE` | quality_scorer.py | Provider record_type metadata |
| `_DEMOTED_TYPES` | quality_scorer.py | Provider record_type metadata |
| `_FUNCTION_WORDS` | quality_scorer.py | Relevance score gate |
| `_TIER_SCORES` (FUZZY/DURATION entries) | quality_scorer.py | Only MBID/ISRC/NONE tiers remain |
| `_SET_NOISE_RE` | soundcloud/adapter.py | Provider metadata |
| `_PLAYCOUNT_MAX_LOG10 = 10.0` | lastfm/adapter.py | Dynamic max from response data |
| `CONTENT_PROVIDERS` list | useArtistContent.ts | Sources from the result card |

## 4. Trade-offs

- **Less merging = more results.** Searching "Bohemian Rhapsody" might show
  Deezer's version AND MusicBrainz's version as separate entries until the
  URL lookup confirms they're the same recording. Acceptable: relevance
  scoring puts the best one first, and the user sees both options.

- **MB URL lookup adds latency.** One HTTP call per result in the enrichment
  phase. Mitigated by: existing enrichment pipeline (top-25 bounded,
  concurrency-capped), aggressive MBID caching, and the 1 req/s MB rate
  limit already handled by the circuit breaker.

- **Underground content without MBIDs never merges.** A SoundCloud upload
  and a Last.fm scrobble for the same unreleased track stay separate.
  Acceptable: both appear, ranked by quality score. The user taps the one
  they want.

- **Keyword-based demotion gone = karaoke may rank higher.** If a provider
  doesn't tag karaoke content with `record_type`, the quality score alone
  determines ranking. Mitigated by: karaoke versions are typically single-
  source with lower popularity than the genuine multi-source recording.

## 5. Implementation path

The foundation rework (discovery-foundation-v1) already built:
- `EntityResolutionTier` enum (just remove FUZZY and DURATION_CONFIRMED)
- `QualityScore` value object + `compute_quality_score`
- Quality score in the sort key
- Content validation cache + quality gate
- Eval harness with 51 golden cases

Changes needed:
1. Strip `_try_merge` to ISRC-only and MBID-only paths (remove JW fallthrough)
2. Add MB URL lookup to the enrichment phase (new `resolve_mbid_from_url` port)
3. Remove `is_demoted` keyword scanning; replace with `record_type` check
4. Remove `significant_tokens` / `_content_tokens` / `_passes_gate`; replace with relevance > 0 gate
5. Update `useArtistContent` to use only sources from the result card
6. Remove `CONTENT_PROVIDERS` static list
7. Run eval harness — accept MRR changes (more separate results = different ranking)
8. Update golden cases for the new merge behavior

## 6. Next

Graduates to `docs/specs/discovery-identity-v1/spec.md` via `/feature-spec`.
