---
date: 2026-06-15
topic: discovery-search-quality
---

# Discovery Search Quality

## Summary

Seven improvements to the Go API's discovery search that close the quality gap with mainstream music apps before production launch: wire popularity signals into ranking, add autocomplete with a music vocabulary, add server-side query correction with transparent UX, parse multi-term queries for intent, deduplicate result variants, and add a recency boost.

---

## Problem Frame

User testing surfaced two classes of search failure that mainstream apps handle well:

**Zero results on typos.** A user searched "Megamsn" intending "Megaman" (a track by Tay-K). All six providers received the raw misspelled query, none returned results, and the user saw an empty screen. Spotify, Apple Music, and Deezer would all have corrected this silently and shown the right track. The current system has no query correction layer — the raw input goes straight to providers with only normalization (lowercasing, diacritics, punctuation) but no spelling awareness.

**Wrong result prominence.** Searching "Megaman" correctly returns an obscure artist literally named "Megaman" above the far more popular track "Megaman" by Tay-K. The ranking pipeline has a popularity slot (step 4 of 8 in the sort key), but it's empty. Deezer returns fan counts and chart ranks that are captured but unused. Last.fm has listener and play count data that isn't even extracted from API responses. The popularity resolver port exists in the codebase but has no implementation wired to it. Text similarity (TokenSortRatio) dominates ranking, so an exact name match for an obscure artist beats a hugely popular track.

Beyond these two failures, the result list lacks polish that users expect from a music search: no autocomplete suggestions, no deduplication of remixes/live versions, no awareness of query structure ("Tay-K Megaman" treated as a bag of words rather than artist + track), and no freshness signal for new releases.

---

## Key Flows

- F1. Autocomplete-assisted search
  - **Trigger:** User starts typing in the search field
  - **Steps:** After a debounce threshold, the client sends the partial query to a suggestion endpoint. The endpoint fuzzy-matches against a music vocabulary (provider charts + search history). Matching suggestions are returned ranked by popularity. The user taps a suggestion or continues typing and submits manually.
  - **Outcome:** Most typos are prevented because the user selects a known-correct term before submitting.
  - **Covered by:** R1, R2, R3

- F2. Corrected search on typo
  - **Trigger:** User submits a query that returns zero results from all providers
  - **Steps:** The server fuzzy-matches the query against the music vocabulary. If a close match is found above a confidence threshold, the server retries the search with the corrected query. Results are returned with a "showing results for [corrected]" indicator and a "search instead for [original]" fallback link.
  - **Outcome:** The user sees relevant results instead of an empty screen, with transparency about what happened.
  - **Covered by:** R4, R5, R6

- F3. Popularity-ranked results
  - **Trigger:** User submits any search query
  - **Steps:** Providers return results with engagement metrics (fan counts, play counts, listener counts). During enrichment, these are normalized to a common popularity scale. The ranking key uses popularity as a tiebreaker within the same relevance band.
  - **Outcome:** "Megaman" by Tay-K ranks above an obscure artist named "Megaman."
  - **Covered by:** R7, R8

---

## Requirements

**Popularity ranking**

- R1. Extract popularity metrics from all providers that offer them: Deezer `nb_fan` and `rank`, Last.fm `listeners` and `playcount`, SoundCloud `playback_count` (from yt-dlp output).
- R2. Normalize extracted popularity metrics to a common scale so cross-provider comparison is meaningful within the ranking key.
- R3. Wire normalized popularity into the existing ranking sort key so that within the same relevance band, more popular results rank higher.

**Autocomplete**

- R4. Provide a suggestion endpoint that accepts a partial query and returns ranked suggestions from the music vocabulary.
- R5. The music vocabulary is seeded from provider chart/top endpoints (Deezer, Last.fm) and grows automatically from successful user search queries over time.
- R6. Autocomplete matching is fuzzy — tolerates partial input, minor misspellings, and missing characters, not just exact prefix matching.

**Server-side query correction**

- R7. When all providers return zero results for a query, the server fuzzy-matches the query against the music vocabulary and retries with the best correction if one exceeds a confidence threshold.
- R8. When correction fires, the response includes a "showing results for [corrected query]" indicator and a "search instead for [original query]" action so the user can override.
- R9. When correction confidence is below threshold or no vocabulary match exists, the system returns the zero-result response as-is rather than guessing.

**Query intent parsing**

- R10. Multi-term queries are analyzed for artist + track patterns (e.g., "Tay-K Megaman") and the ranking boosts results matching both the detected artist and track fields over results matching either alone.
- R11. Intent parsing is best-effort and does not break single-term or ambiguous queries — when no pattern is detected, the query is treated as a bag of words (current behavior).

**Version deduplication**

- R12. When multiple versions of the same recording appear in results (original, remix, live, acoustic, remaster), they are collapsed into one representative entry — the most popular version.
- R13. The existence of other versions is indicated (e.g., count or label) so the user knows variants exist without them cluttering the result list.

**Recency boost**

- R14. Results with recent release dates receive a small ranking boost so that new releases surface when searching for an artist or album, even when their all-time play counts are still low.
- R15. The recency signal is a minor tiebreaker, not dominant — a classic hit with millions of plays should not be displaced by a new release with minimal engagement.

---

## Acceptance Examples

- AE1. **Covers R7, R8.** Given a user searches "Megamsn," when all providers return zero results, then the server corrects to "Megaman," retries the search, and returns results with "Showing results for Megaman" and a "Search instead for Megamsn" option.
- AE2. **Covers R1, R2, R3.** Given a user searches "Megaman," when results include both the track "Megaman" by Tay-K and an obscure artist named "Megaman," then the Tay-K track ranks higher because its popularity metrics (play counts, fan counts) far exceed the artist's.
- AE3. **Covers R4, R5, R6.** Given a user types "Mega" in the search field, when autocomplete fires, then suggestions include "Megaman - Tay-K" (or similar popular matches) drawn from the music vocabulary.
- AE4. **Covers R10, R11.** Given a user searches "Tay-K Megaman," when results are ranked, then results matching both "Tay-K" as artist and "Megaman" as title rank above results matching only one of those terms.
- AE5. **Covers R12, R13.** Given the providers return the original "Megaman" by Tay-K plus a remix and a slowed version, then the result list shows one entry (the most popular version) with an indication that other versions exist.
- AE6. **Covers R14, R15.** Given an artist just released a new album this week, when a user searches for that artist, then the new album appears in results even with low play counts — but it does not displace the artist's all-time most popular tracks.
- AE7. **Covers R9.** Given a user searches "xkqzwp" (gibberish with no vocabulary match), when no correction meets the confidence threshold, then the system returns zero results without attempting a bad correction.

---

## Success Criteria

- Searching "Megamsn" returns "Megaman" by Tay-K (or relevant results) instead of an empty screen.
- Searching "Megaman" ranks the Tay-K track above obscure same-name artists.
- Autocomplete suggestions appear for partial queries and include popular matches.
- Multi-term searches like "Tay-K Megaman" surface the specific track as the top result.
- Result lists are noticeably less cluttered due to version deduplication.
- Search quality feels comparable to mainstream music apps in manual testing across a diverse set of queries.

---

## Scope Boundaries

**Deferred — revisit if these improvements aren't sufficient:**

- ML/NLP-based query understanding (e.g., trained models for intent classification, semantic search, embedding-based similarity). The current approach uses pattern matching and edit-distance heuristics. If search quality plateaus, ML is the natural next step — start with a query correction model trained on search logs.
- Personalized ranking based on user listening history, favorites, or past searches. Could significantly improve result relevance per-user but requires behavioral data infrastructure.
- Full MusicBrainz database dump as a vocabulary source. Charts + search history cover the head and long tail respectively. MusicBrainz adds comprehensive coverage of obscure terms but at meaningful storage and refresh cost. Add it if gap analysis shows frequent misses on terms that aren't in charts or history.
- Adding new search providers (Spotify API, YouTube Music API). Would increase source diversity and potentially improve fuzzy search coverage, but each provider adds integration and maintenance cost.
- Search analytics dashboard and A/B testing infrastructure. Would enable data-driven iteration on ranking weights and correction thresholds, but is pre-production overhead.
- Offline/local search index (e.g., Elasticsearch, Meilisearch) for server-side fuzzy matching independent of providers. Heavy infrastructure for a solo project, but would give full control over search quality.

**Not in scope:**

- Changes to the frontend search UI beyond the autocomplete dropdown and "showing results for" indicator.
- Changes to provider integration (new providers, different API endpoints) beyond extracting existing unused fields.
- Changes to the dedup/merge strategy (identifier-only merge is settled per discovery-identity-v1).
- Eval harness or golden query set (valuable but a separate effort; covered in discovery-foundation-v1 spec).

---

## Key Decisions

- **Vocabulary source: provider charts + search history (layers 1+2).** MusicBrainz dump deferred. Charts cover popular terms; search history captures the long tail of actual user queries. The vocabulary serves both autocomplete and query correction — a single shared data source.
- **Correction is automatic, not "did you mean."** Matches how Spotify and Apple Music handle typos. The "showing results for X" + "search instead for Y" UX provides transparency without requiring user action.
- **Popularity normalization across providers.** Different providers use different scales (Deezer fan counts vs Last.fm play counts vs SoundCloud plays). Normalizing to a common scale before feeding into the ranking key ensures fair cross-provider comparison.
- **Recency as a minor signal.** Prevents new releases from being invisible but doesn't let a brand-new track with 100 plays outrank a classic with millions.

---

## Dependencies / Assumptions

- Provider chart/top endpoints (Deezer, Last.fm) are available and return enough data to seed a useful vocabulary. These need to be verified during planning.
- Last.fm API responses include `listeners` and `playcount` fields that can be extracted without additional API calls. Needs verification.
- SoundCloud's yt-dlp output includes `playback_count`. Needs verification.
- The existing ranking key structure can accommodate the new signals without a full rewrite.
- Autocomplete requires a fast lookup mechanism (sub-100ms). The storage and retrieval strategy is a planning decision.

---

## Outstanding Questions

### Deferred to Planning

- [Affects R4, R6][Technical] What data structure and storage backend should back the autocomplete vocabulary? Options include Redis sorted sets, in-memory trie, or trigram index.
- [Affects R2][Technical] What normalization formula maps different provider popularity scales to a common range? Log-scale, percentile-based, or min-max normalization each have tradeoffs.
- [Affects R7][Technical] What edit-distance or phonetic algorithm gives the best correction accuracy for music terms? Levenshtein, Damerau-Levenshtein, Double Metaphone, or a combination.
- [Affects R10][Needs research] What heuristics reliably detect "artist + track" vs "track + artist" vs "two-word title" patterns without a trained model?
- [Affects R12][Technical] What similarity threshold and fields should define "same recording, different version" for dedup? Title + artist + duration proximity, or title normalization (strip "remix", "live", "acoustic" suffixes)?
- [Affects R5][Technical] How frequently should chart data be refreshed, and what's the retention policy for search history entries in the vocabulary?
- [Affects R14][Technical] What time window and boost magnitude constitute "recent" for the recency signal?
