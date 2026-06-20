---
date: 2026-06-20
topic: discovery-pipeline-clarity
---

# Discovery Pipeline Clarity

## Summary

Replace the identity resolution system with multi-provider consensus for artist discography validation. Expand existing provider adapters (Last.fm, Wikidata, MusicBrainz, Discogs) and add two new ones (Tidal, Cover Art Archive). Restructure the artwork chain to prioritize ID-based resolution over name-search. Fix remaining audit bugs. Document the pipeline with per-stage contracts and diagnostic logging.

---

## Problem Frame

The discovery pipeline has grown through many iterations of fixing specific artist queries. The identity resolution system — profile building, ISRC registrant checking, genre filtering, temporal checks — was built to handle discography contamination (wrong-artist albums appearing on an artist's detail page). It works for common-name artists like "che" but destroys results for underground artists (OsamaSon, Killeastsxde) who have sparse metadata.

Three concrete failures surfaced during real-world testing:

**Search-page vs detail-page divergence.** Searching "Killeastsxde" shows all his albums on the search page (via `FindRelatedService`, raw Deezer data). Tapping his artist profile shows 2 singles (via `GetArtistContentService`, filtered through identity resolution). Same data source, different processing.

**Over-aggressive identity resolution.** The system has no positive signals for underground artists (no MusicBrainz presence, no iTunes presence, different distributors per release), and constraint checks (genre, temporal, ISRC) produce false positives.

**Profile poisoning.** MusicBrainz name-matching can resolve to the wrong artist, poisoning every downstream check.

During grilling, a deeper insight emerged: **the identity resolution system is the wrong abstraction.** It tries to answer "is this album wrong?" by building artist profiles and checking constraints — an approach that has inherent false-positive risk and relies on hardcoded thresholds. The generic alternative is **multi-provider consensus**: instead of asking "is this album wrong?", ask "do multiple independent sources agree this album belongs to this artist?" Albums confirmed by 2+ providers are definitively correct. Albums from only one provider are included but unconfirmed. Only albums that MusicBrainz explicitly credits to a different artist (different MBID) are removed.

Separately, artwork resolution suffers from wrong-artist photos because the chain searches by name, not by provider-specific ID. ID-based artwork (Deezer's own images, Fanart.tv with MBID, Cover Art Archive) is always correct for the given entity. Name-based search (Genius, TheAudioDB, iTunes) can match the wrong artist.

---

## Actors

- A1. Developer (solo): builds, debugs, and evaluates the discovery pipeline
- A2. App user: searches for music, taps artist results to see discography and top tracks
- A3. Pipeline stages: the discrete processing steps that transform raw provider data into ranked results

---

## Key Flows

- F1. Search flow (existing, to be documented with stage contracts)
  - **Trigger:** User types a query in the search bar
  - **Actors:** A2, A3
  - **Steps:** Query cleanup, intent detection, scatter to providers (now including Tidal), fuse/merge on identifiers, normalize popularity (now including Last.fm play counts + YouTube view counts), score/rank, collapse/diversity, enrich (artwork via ID-first chain), rerank, find related
  - **Outcome:** Ranked results with related groups displayed on the search page
  - **Covered by:** R7, R8, R9, R10, R11

- F2. Artist detail flow (redesigned)
  - **Trigger:** User taps an artist result from the search page
  - **Actors:** A2, A3
  - **Steps:** Fetch albums from multiple providers in parallel (Deezer + MB + Discogs + Last.fm + iTunes + Tidal), build consensus (match albums across providers by normalized title), mark each album as confirmed (2+ providers) or unconfirmed (1 provider), apply MB safety check (only remove albums MB explicitly credits to a different artist), fetch top tracks, return content
  - **Outcome:** Artist's discography with consensus-backed results, top tracks, displayed on detail page
  - **Covered by:** R1, R2, R3, R4, R5, R6

- F3. Artwork resolution flow (restructured)
  - **Trigger:** Enrichment stage during search, or detail page rendering
  - **Actors:** A3
  - **Steps:** Try ID-based artwork first (Deezer own image → Cover Art Archive with MBID → Fanart.tv with MBID), fall back to name-based search only as last resort (Genius → TheAudioDB → iTunes)
  - **Outcome:** Correct artwork for the entity, not a name-collision wrong image
  - **Covered by:** R12, R13

---

## Requirements

**Multi-provider consensus (replaces identity resolution)**

- R1. The artist detail page must fetch album lists from multiple providers in parallel (Deezer, MusicBrainz, Discogs, Last.fm, iTunes, Tidal — whichever are available) and build a consensus view.
- R2. Albums appearing in 2+ provider album lists (matched by normalized title) are marked as confirmed. Albums appearing in only 1 provider are included but marked as unconfirmed. No album is removed based on consensus alone — "unconfirmed" is informational, not a filter.
- R3. The only removal mechanism is MB identity contradiction: if MusicBrainz explicitly credits an album to a different artist (different MBID than the artist being viewed), that album is removed. "MB doesn't know this album" is NOT grounds for removal.
- R4. Search page and detail page must return consistent data. If the search page's related groups show an artist's albums, the detail page must show at least the same albums.
- R5. Default album limit for the artist albums endpoint must be raised from 10 to 50.
- R6. The identity resolution system (identity_resolver.go, identity_constraints.go, identity_cache.go) and its heuristic checks (genre, temporal, ISRC registrant, artist type) are replaced by consensus. These files become unused and should be removed.

**Provider expansion**

- R7. Add Tidal as a new search provider and artist content provider (artist albums, top tracks, ISRC lookup). OAuth 2.0 client credentials, open registration.
- R8. Add Cover Art Archive as an artwork source. MBID-keyed album art at up to 1200px, no rate limit. Requires MBID (from MusicBrainz or Wikidata bridge).
- R9. Expand Last.fm adapter: add `artist.getTopAlbums` for consensus and `artist.getTopTracks` for popularity data (real play counts).
- R10. Expand Wikidata bridge: use Deezer→MBID resolution during search merging (currently only used for artwork). Enables MBID-based entity merging for Deezer artist results.
- R11. Expand YouTube adapter: use video view counts as a popularity signal during enrichment, not just artwork.

**Artwork chain restructure**

- R12. Artwork resolution must prioritize ID-based sources over name-based search. Order: Deezer own image (always correct for that ID) → Cover Art Archive (MBID-keyed) → Fanart.tv (MBID-keyed) → name-based fallback (Genius → TheAudioDB → iTunes).
- R13. For artist images specifically: Deezer `picture_big` → Fanart.tv artist thumb → TheAudioDB → Genius → YouTube channel thumbnail. Name-search resolvers are last resort only.

**Pipeline documentation and diagnostics**

- R14. Each pipeline stage must have a defined input type and output type documented in ARCHITECTURE.md.
- R15. The search-path and detail-path must be documented as separate pipelines in ARCHITECTURE.md with Mermaid diagrams.
- R16. Diagnostic logging must emit a structured log entry per stage (stage name, input count, output count, items changed). Debug log level.

**Self-correction**

- R17. Wire the existing ClickSignalProvider port to persistence and make click data available to the ranking pipeline. Click signals inform ranking but do not block the search flow.

**Search pipeline cleanup**

- R18. Provider RRF weights must be equalized to 1.0 for all providers. RRF becomes a pure "how many providers agree" signal. Popularity differentiation is handled by the popularity normalization stage, not by RRF.

**Remaining audit fixes**

- R19. `dedupAlbums` must include artist name (subtitle) in the dedup key, not just title.
- R20. Error responses from GetTopTracks and GetAlbums must initialize Items to an empty slice, not nil.
- R21. `maxProviderLookups` in FindRelatedService should be evaluated for increase (currently 3).

---

## Acceptance Examples

- AE1. **Covers R1, R2, R4.** Given a search for "Killeastsxde", when the user taps his artist profile, the detail page shows all albums that Deezer returns. Albums also found on Last.fm or MusicBrainz are marked confirmed. No albums are removed.
- AE2. **Covers R1, R2.** Given a search for "OsamaSon", when the user taps his artist profile, the detail page shows all albums from Deezer. OsamaSon has no MusicBrainz entry, so no MB contradiction check fires. All albums are shown, some may be unconfirmed.
- AE3. **Covers R2, R3.** Given a search for "che", when the user taps the artist profile, Deezer returns 20 albums. MusicBrainz confirms 12 of them belong to this "che" (matching MBID) and identifies 4 as belonging to a different "Che" (different MBID). The 4 contamination albums are removed. The remaining 4 (unknown to MB) are kept as unconfirmed.
- AE4. **Covers R3.** Given an artist where MusicBrainz resolves to the wrong person (zero overlap between MB's confirmed titles and Deezer's album list), the MB identity is discarded. No albums are removed. All Deezer albums are shown.
- AE5. **Covers R12, R13.** Given an artist with an MBID, artwork resolution tries Deezer's own image first, then Cover Art Archive, then Fanart.tv. Name-based search (Genius, TheAudioDB) only fires if all ID-based sources return nothing.
- AE6. **Covers R7.** Given a search query, Tidal is included in the scatter-gather alongside existing providers. Tidal results merge with other providers' results via ISRC or MBID.
- AE7. **Covers R16.** Given a search query with debug logging enabled, the logs show one structured entry per pipeline stage with stage name, input count, and output count.

---

## Success Criteria

- Searching "OsamaSon", "Killeastsxde", and "che" all produce correct artist detail pages — underground artists show their full discography, common-name artists have contamination removed via MB identity contradiction only
- No hardcoded thresholds, genre maps, ISRC registrant checks, or temporal heuristics in the album filtering path
- Artwork for artists and albums is correct (matches the entity being viewed, not a name-collision different artist)
- The detail page queries multiple providers in parallel and builds a consensus view
- ARCHITECTURE.md contains Mermaid diagrams for both search and detail flows with stage contracts
- Each pipeline stage emits diagnostic logs at debug level
- Click signals are persisted and available for ranking
- Tidal and Cover Art Archive are integrated and contributing to consensus/artwork

---

## Scope Boundaries

- ML model implementation (deferred until consensus approach is evaluated)
- Personalization layer (user-specific ranking based on listening history)
- Mobile-side UI changes (confirmed/unconfirmed badges on albums are a future mobile feature)
- Paid provider integrations (Spotify, Apple Music)
- SoundCloud official API (deprecated, yt-dlp scraping stays as-is)
- Elasticsearch or search engine adoption
- AcoustID audio fingerprinting (relevant to import/acquisition, not discovery)

---

## Key Decisions

- **Multi-provider consensus replaces identity resolution:** Instead of building artist profiles and filtering with heuristics (genre, temporal, ISRC), query 6 providers for album lists and build consensus. This is generic (works the same for all artists), has no tunable thresholds, and fails safe (worst case: unconfirmed albums are shown, never correct albums removed). The only removal mechanism is MB identity contradiction (album explicitly credited to a different MBID).
- **ID-based artwork first, name-search last:** The wrong-artist photo problem is caused by name-based artwork search matching the wrong artist. Prioritizing ID-based sources (Deezer own image, Cover Art Archive with MBID, Fanart.tv with MBID) eliminates this for entities with known IDs. Name search is the fallback, not the default.
- **Tidal is the strongest single provider addition:** Free, open registration, structured catalog, ISRC support, artist albums + top tracks. No other new free provider comes close.
- **Cover Art Archive fills the artwork gap:** Free, no rate limit, up to 1200px, MBID-keyed. Best free album art source when MBIDs are available.
- **Click signals wired now, impact later:** The ClickSignalProvider port already exists. Wiring to persistence starts accumulating behavioral data immediately.
- **Identity resolution system is removed, not refactored:** The heuristic approach (genre checks, temporal checks, ISRC registrant matching) has inherent false-positive risk and relies on hardcoded thresholds. Consensus is the replacement, not an addition alongside it.
- **Equal RRF weights across all providers:** Provider RRF weights removed (all 1.0). RRF becomes a pure agreement signal ("how many providers agree this is relevant") rather than a judgment about provider quality. Popularity differentiation is handled by the separate popularity normalization stage. This also avoids needing to assign arbitrary weights when new providers (Tidal) are added.
- **Consensus-based removal when agreement is strong:** When 4+ providers have data for an artist and an album appears on NONE of them except Deezer, it is removed as a consensus rejection. "Provider has data" means the provider returned a non-empty album list for this artist — providers that returned nothing (don't know this artist) don't count. This is data-driven (cross-provider agreement), not heuristic (genre/temporal/ISRC checks).

---

## Dependencies / Assumptions

- Deezer's `/artist/{id}/albums` returns the primary album list (other providers supplement for consensus)
- Tidal's developer portal remains free and open for registration
- Cover Art Archive (hosted by Internet Archive) remains available with no rate limits
- Redis is available for click signal persistence (existing infrastructure)
- MusicBrainz rate limit (1 req/sec) is manageable for consensus queries (one lookup per artist detail page view, cacheable)
- The mobile app will need updates to display confirmed/unconfirmed status on albums (out of scope for this doc)

---

## Outstanding Questions

### Deferred to Planning

- [Affects R1][Technical] How should consensus matching handle title variations across providers? Normalized title comparison may miss "Greatest Hits" vs "Greatest Hits (Deluxe Edition)". Fuzzy matching threshold needs to be determined during implementation.
- [Affects R1][Technical] What is the latency budget for multi-provider consensus? Querying 6 providers in parallel — what timeout per provider, and should results render progressively?
- [Affects R3][Technical] How does MB identity contradiction work when the artist has no MBID at all? If neither the artist nor the album have MBIDs, the contradiction check cannot fire. Confirm this defaults to "keep all."
- [Affects R7][Needs research] Tidal API: verify ToS compatibility for non-commercial music manager use. Review developer terms before implementation.
- [Affects R10][Technical] Wikidata SPARQL latency: is the Deezer→MBID bridge fast enough to use during search merging, or should it be cached aggressively?
- [Affects R17][Technical] Minimum click volume before click signals should influence ranking. Needs measurement once data is flowing.
- [Affects R16][Technical] Should diagnostic logging use structured slog attributes or a separate diagnostic event system? Resolve based on existing logging patterns.

See also: [Provider Inventory](2026-06-20-provider-inventory.md) for the complete provider capability matrix.
