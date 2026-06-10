# Identifier-Only Merge

> Spec for `discovery-identity-v1` — version 1, drafted 2026-06-10.
> Authors: solo + Claude.
> Status: Draft.

## Problem

The discovery merge pipeline uses Jaro-Winkler similarity thresholds, keyword regex lists, stopword sets, and static provider priors to decide which results are the same entity. These hardcoded heuristics produce wrong merges for common/short names (searching "Che" blends discographies from unrelated artists), require manual keyword additions for each new edge case, and can't scale. Every provider already solved identity within their own system — Deezer knows artist 234701081, MusicBrainz knows its MBIDs — but we re-derive identity from text similarity instead of trusting provider signals.

## User value

Search results are always correctly identified. Two artists named "Che" never contaminate each other's discography. No new edge case requires a code change — the system works on any query, any kind, any provider, because identity comes from authoritative identifiers (ISRC, MBID), not from fuzzy text matching.

## Scope tier / MVP cut

- **Minimal (ship this):**
  - Strip merge pipeline to ISRC-only and MBID-only paths (remove all JW/duration heuristics)
  - Add MusicBrainz URL lookup for cross-provider identity bridging
  - Replace keyword-based demotion with provider `record_type` metadata
  - Replace token-based match gate with relevance-score gate
  - Constrain artist discography to sources on the tapped result card
  - Remove all 13 identified heuristic constants
  - Update eval harness for new merge behavior
- **Deferred to post-launch:**
  - Wikidata SPARQL bridge (broader coverage than MB URL lookup alone)
  - User-driven identity linking ("these are the same artist" UI)
- **Justified exceptions:** MusicBrainz URL lookup is a new API call pattern, needed now because it's the only zero-heuristic cross-provider identity bridge. Without it, results from different providers never merge.

## Acceptance criteria

### Identifier-only merge

1. **AC#1** — Given two results from different providers sharing the same ISRC, when fuse_and_rank runs, then they merge into one result.
2. **AC#2** — Given two results from different providers sharing the same MBID, when fuse_and_rank runs, then they merge unconditionally (MBID is authoritative — no title similarity check).
3. **AC#3** — Given two results from different providers with the same title and artist but NO shared ISRC or MBID, when fuse_and_rank runs, then they remain as separate results.
4. **AC#4** — Given the codebase after this spec ships, when grepping for `_JW_HIGH`, `_JW_MEDIUM`, `_JW_ARTIST`, `_DURATION_TOLERANCE_S`, `_COVER_PHRASE_RE`, `_COVER_WORD_RE`, `_DEMOTED_TYPES`, `_FUNCTION_WORDS`, `_SET_NOISE_RE`, `_fallback` (provider priors dict), `_PLAYCOUNT_MAX_LOG10`, `CONTENT_PROVIDERS` (static provider list), and `_TIER_SCORES` entries for FUZZY/DURATION_CONFIRMED, then none exist anywhere. The `_winning_prior` function is replaced by quality score for both canonical selection and sort key.

### MusicBrainz URL lookup bridge

5. **AC#5** — Given a search result with a Deezer artist source URL, when the enrichment phase queries MusicBrainz `/ws/2/url?resource=<deezer_url>&inc=artist-rels`, then the resolved MBID (if any) is stored in `extras["mbid"]`.
6. **AC#6** — Given two results from Deezer and Last.fm that both resolve to the same MBID via URL lookup, when fuse_and_rank runs after enrichment, then they merge.
7. **AC#7** — Given a result whose provider URL has no MusicBrainz link, when the URL lookup returns empty, then the result keeps its original sources and is not merged with anything.

### Provider-metadata demotion

8. **AC#8** — Given a result with `extras["record_type"]` NOT in the canonical set `{"album", "single", "ep"}` (e.g., "compilation", "live", "remix", "demo", or any other value), when ranking runs, then it is demoted below results with canonical `record_type` within the same relevance band. Results with no `record_type` are NOT demoted.
9. **AC#9** — Given a result titled "Song (Karaoke Version)" with NO `record_type` metadata, when ranking runs, then it is ranked by quality score alone — no keyword-based demotion.

### Relevance-score gate

10. **AC#10** — Given a search result with zero relevance to the query (no fuzzy text overlap), when fuse_and_rank runs, then it is filtered out.
11. **AC#11** — Given a search result with any non-zero relevance to the query, when fuse_and_rank runs, then it passes the gate (no stopword or token-length filtering).

### Primary-source discography

12. **AC#12** — Given an artist result with `sources: [deezer/234701081]` (single source), when the user taps into the discography, then only Deezer is queried for albums and top tracks.
13. **AC#13** — Given an artist result with `sources: [deezer/234701081, musicbrainz/0a68f3b5]` (MBID-confirmed multi-source), when the user taps into the discography, then both Deezer and MusicBrainz are queried.
14. **AC#14** — Given any artist result, when the discography is fetched, then SoundCloud is NOT queried by artist name — only by a SoundCloud source ID if present on the result card.

### Eval harness

15. **AC#15** — Given the updated eval harness with golden cases adjusted for identifier-only merge behavior, when the ranker runs, then MRR@10 does not regress below 0.85 (lower than the 0.93 baseline because less merging produces more separate results, but relevance ranking keeps the best on top).

## Out of scope

- Wikidata SPARQL bridge (future enrichment — broader coverage)
- User-driven identity linking ("these are the same artist" UI)
- New provider integrations
- Artist profile images
- Playback integration
- Any new hardcoded constants, thresholds, or keyword lists

## Design considerations

- [vault: wiki/concepts/Anti-Corruption Layer Pattern.md] — each provider adapter is an ACL. Provider IDs are the authoritative identity within their system. The merge layer respects this authority rather than overriding it with fuzzy text matching.
- The MusicBrainz URL lookup (`/ws/2/url?resource=<url>`) is the cross-system identity bridge — it resolves a provider-specific URL to a canonical MBID without any string matching. Coverage is community-curated: good for major artists/releases, sparse for underground tracks.
- The `normalize.py` regexes (bracket stripping, feature token normalization, article stripping) remain — they're text canonicalization for display and relevance scoring, not identity heuristics.

High-level approach:

- This reworks the `discovery` bounded context's application-layer merge logic.
- It removes `EntityResolutionTier.FUZZY` and `EntityResolutionTier.DURATION_CONFIRMED` — only MBID, ISRC, and NONE remain.
- It adds a new port: `MbidResolver` (resolves provider URLs to MBIDs via the MusicBrainz URL endpoint).
- Frontend: `useArtistContent` constrains to sources on the result card instead of fanning out by name.

## Dependencies

- **Bounded contexts**: `discovery` (existing)
- **Other features**: `discovery-foundation-v1` (just shipped — quality scorer, entity resolution, quality gates)
- **External services**: MusicBrainz URL lookup API (existing adapter, new endpoint)
- **Library/framework additions**: none

## Risks / open questions

- **Risk**: Less merging means more results for common queries. Mitigation: relevance scoring puts the best result first; the user sees more options but the right one is on top.
- **Risk**: MusicBrainz URL lookup coverage is sparse for underground content. Mitigation: results without MBID links simply don't merge — they appear separately, ranked by quality score.
- **Risk**: Removing keyword-based demotion may let karaoke/cover results rank higher when providers don't tag `record_type`. Mitigation: karaoke versions are typically single-source with lower popularity; quality score handles this. Eval harness catches regressions.
- **Risk**: MusicBrainz rate limit (1 req/s) constrains URL lookup throughput. Mitigation: lookup runs in the existing enrichment phase (top-25, concurrency-capped), results are cached aggressively.

## Telemetry

- **Log events**:
  - `mbid_url_lookup` — per result: provider URL queried, MBID resolved (or null), latency
  - `identifier_merge` — per merge: merge type (isrc/mbid), provider pair, result signature
  - `no_merge_same_name` — when two results share a name but have no shared identifier (frequency tracking)
- **Metrics**:
  - `discovery.merge_rate` — % of results merged per query (expect decrease vs baseline)
  - `discovery.mbid_lookup_hit_rate` — % of URL lookups that resolve to an MBID
  - `discovery.results_per_query` — average result count (expect increase vs baseline)
- **Alerts**:
  - MBID lookup hit rate < 10% sustained — investigate whether URLs are malformed
  - Eval harness MRR regression below 0.85

## Related

- [vault: wiki/concepts/Anti-Corruption Layer Pattern.md] — provider adapters as ACL
- Related ADR: `docs/adr/0007-unified-music-search.md`
- Predecessor: `docs/specs/discovery-foundation-v1/spec.md`
- Brainstorm: `docs/brainstorms/2026-06-10-identifier-only-merge.md`
