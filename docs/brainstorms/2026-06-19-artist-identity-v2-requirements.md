---
date: 2026-06-19
topic: artist-identity-v2
parent: 2026-06-18-discovery-detail-residuals.md
---

# Artist Identity v2 — Artwork + Discography Quality

## Summary

Solve the two interconnected same-name-artist problems — wrong artwork and contaminated discographies — by adding Discogs and YouTube as identity-verified sources, shifting discography filtering from "pessimistic exclude" to "optimistic include with heuristic removal", and designing a fingerprinting port for future AcoustID integration. Build for extensibility so future sources (SerpAPI, SoundCloud, ML-based approaches) slot in trivially.

---

## Problem Frame

Deezer conflates multiple real-world artists under one ID when they share a name. "Che" (Atlanta rapper, born 2006, Deezer ID 234701081) shows 82 releases including albums by a stoner rock band, an Estonian singer, and a Spanish artist. The current artwork chain (Deezer → TheAudioDB → iTunes → Fanart.tv → Genius) fails completely for this artist:

- Deezer: placeholder image (empty MD5 hash)
- TheAudioDB: returns image for "Ché", a stoner rock band — **completely wrong artist**
- Fanart.tv: empty response for this MBID
- iTunes: no artist images
- Genius: not reached (earlier resolver returned the wrong image)

The current discography fix (MB cross-reference, confirmed vs unconfirmed ordering) is wrong in both directions: it keeps contamination visible (just deprioritized) and wrongly classifies real albums MB hasn't catalogued (e.g., "REST IN BASS: ENCORE") as unconfirmed.

Deep research confirmed: Google CSE (closed to new users), Bing Image Search (retired Aug 2025), Last.fm images (broken since 2019), and Apple MusicKit (no artist artwork) are all dead ends. Spotify's 2026 API lockdown makes it impractical for a solo project.

---

## Requirements

### New provider: Discogs (artwork + discography)

- **R1.** Add Discogs as an artwork resolver in the enrichment chain. Discogs has community-curated artist images and separate entries per same-name artist ("Che", "Che (2)", "Che (3)").
- **R2.** Resolve the Deezer artist to a Discogs artist ID using album-title overlap cross-referencing (same approach as the existing MB artist resolution).
- **R3.** Cache the Discogs artist ID resolution (Redis, 30-day TTL) to avoid repeated lookups.
- **R4.** Use Discogs genre/year/country data as heuristic signals for discography filtering (see R11-R14).

### New provider: YouTube Data API (artwork fallback)

- **R5.** Add YouTube as an artwork resolver in the enrichment chain, after Discogs. Search for the artist's YouTube channel using "artist name + disambiguation" (e.g., "Che Atlanta rapper"). Use the channel thumbnail (up to 800×800) as artwork.
- **R6.** YouTube channel search must disambiguate: use the disambiguation text from MusicBrainz when available, fall back to genre/city context.
- **R7.** Respect YouTube's 10,000 unit/day free quota (search.list = 100 units, ~100 lookups/day). Cache results aggressively (30-day TTL).

### Artwork chain ordering

- **R8.** Updated artwork resolution order: Fanart.tv (MBID-based, authoritative) → Discogs (identity-verified) → YouTube (artist-uploaded) → Genius → Deezer → TheAudioDB. MBID-based resolvers first, name-based last.
- **R9.** TheAudioDB must be demoted to last position. It searches by name only with no disambiguation and returns wrong-artist images for common names.

### Discography: optimistic include with heuristic removal

- **R10.** Shift from "confirmed vs unconfirmed" ordering to "keep unless positive evidence of mismatch". Albums are included by default. Contamination is removed, not deprioritized.
- **R11.** **Release year floor**: if the artist's birth year is known (from MB: 2006 for Che), remove albums released before they could plausibly have been active (year < birth_year + 12). This kills 1990s-era contamination.
- **R12.** **Genre cluster mismatch**: if the artist's genre is known (from Discogs or MB), flag albums whose genre tags strongly conflict (hip-hop artist → stoner rock album). Require 2+ mismatch signals before removal to avoid false positives.
- **R13.** **Discogs cross-reference**: if the artist has a resolved Discogs ID (R2), cross-reference album titles against Discogs discography. Albums confirmed by Discogs are kept. Albums absent from both MB and Discogs with heuristic mismatches are removed.
- **R14.** **MB text search fallback**: for albums not in MB's release-group list for the artist's MBID, search MB by `artist:Che releasegroup:<title>` to check if the album exists under a different MBID. This catches albums MB has but hasn't linked to this specific MBID yet.

### Fingerprinting port (design now, implement later)

- **R15.** Define an `AudioFingerprinter` port interface that accepts audio data and returns artist identity (MBID). Do not implement it yet.
- **R16.** When the acquisition pipeline downloads a track, the fingerprinting port can be called to verify the track belongs to the expected artist. This is the long-term authoritative source — deferred until the acquisition pipeline is mature.

### Extensibility

- **R17.** New artwork resolvers must implement the existing `ArtworkResolver` port and plug into `ChainedArtworkResolver` with no changes to the orchestrator.
- **R18.** New discography validators must implement the existing `AlbumValidator` port pattern.
- **R19.** Adding a new source should require: one adapter file, one wiring line in `app.go`, and (optionally) one cache adapter. No orchestrator changes.

---

## Acceptance Examples

- **AE1 (artwork).** Search "Che". The artist result shows a real photo of the Atlanta rapper — from Discogs or YouTube, not TheAudioDB's stoner rock band image.
- **AE2 (discography).** Tap "Che" artist. Discography shows REST IN BASS, Sayso Says, closed captions, REST IN BASS: ENCORE, Fully Loaded. Does NOT show Samsonite, Gallos Ciegos, Tšernobõl, Kiss Me in the Sky.
- **AE3 (genre diversity).** Search "Aurora". Artist detail shows correct Norwegian pop discography, not contamination from other artists named Aurora. (Validates the system works beyond hip-hop.)
- **AE4 (extensibility).** Adding a future SerpAPI artwork resolver requires only a new adapter file + one line in `app.go`.
- **AE5 (fingerprinting port).** The `AudioFingerprinter` port interface exists in `ports/ports.go` but has no implementation. No runtime behavior changes.

---

## Success Criteria

- "Che" detail screen shows a real photo of the Atlanta rapper (not a rock band).
- "Che" discography shows only albums by the correct artist — contamination removed, not deprioritized.
- "REST IN BASS: ENCORE" (real album not in MB) is preserved, not filtered out.
- No regression on mainstream artists (Drake, Kendrick, Bad Bunny) — they already work.
- Discogs and YouTube API keys are configured via env vars, matching the existing provider pattern.
- All existing discovery tests continue to pass.

---

## Scope Boundaries

**In scope:**
- Discogs adapter (artwork + discography verification)
- YouTube Data API adapter (artwork)
- Heuristic discography filtering (year floor, genre mismatch, cross-reference)
- Artwork chain reordering
- AudioFingerprinter port definition (interface only)
- Provider extensibility cleanup

**Deferred for later:**
- SerpAPI integration (paid, add when free sources prove insufficient)
- SoundCloud API integration (needs Artist Pro subscription)
- AcoustID fingerprinting implementation (needs acquisition pipeline maturity)
- ML-based audio similarity — Spotify's "Which Witch" approach. Deferred not rejected. Revisit when team grows. ([memory: ml-audio-approach])
- User-uploaded artist images

**Out of scope:**
- Spotify API (2026 lockdown makes it impractical)
- Google CSE (closed to new users, retiring Jan 2027)
- Bing Image Search (retired Aug 2025)

---

## Key Decisions

- **Optimistic include over pessimistic exclude** for discography. The Spotify research paper on this exact problem ("Which Witch", 2023) confirms: systems that filter by default produce false negatives for new/uncatalogued releases. Keep unless positive mismatch evidence.
- **Discogs as the secondary identity anchor** after MusicBrainz. Discogs has separate entries per same-name artist, community-curated images, and the best underground coverage of any structured database.
- **YouTube for artist-uploaded artwork** rather than web scraping or paid APIs. Channel thumbnails are the artist's own brand photo, updated by them, and available via a free API.
- **TheAudioDB demoted to last** in the artwork chain. It searches by name with no disambiguation and is the direct cause of the wrong-artist-image bug.
- **Zero monthly cost** for this iteration. SerpAPI ($25/month) deferred to a future upgrade.
- **Genre-agnostic design** — the system serves multiple users with diverse music tastes. Heuristics must not assume hip-hop-specific patterns. ([memory: multi-user-music-diversity])

---

## Dependencies / Assumptions

- Discogs API requires authentication (OAuth 1.0 or personal token). Free tier: 60 req/min authenticated, 25 req/min unauthenticated.
- YouTube Data API v3 requires a Google Cloud project + API key. Free tier: 10,000 units/day (~100 channel lookups).
- MusicBrainz birth year data is available for some artists (confirmed: Che has `begin-date: 2006-08-29`). When absent, release year heuristic cannot fire — acceptable degradation.
- Discogs artist resolution (Deezer→Discogs ID mapping) will need the same album-title-overlap approach used for MB resolution. This may require a separate Discogs search call.
- The existing `ArtworkResolver` and `AlbumValidator` port interfaces are sufficient for Discogs and YouTube — no port changes needed.
- REST IN BASS: ENCORE is a real Che album that MusicBrainz hasn't catalogued. The optimistic-include approach preserves it by default. Over time, MB community editors will add it.

---

## Outstanding Questions

### Deferred to Planning

- [Affects R2] How to resolve Deezer artist → Discogs artist ID. Name search returns multiple "Che (N)" entries. Album-title overlap is the likely approach but needs design.
- [Affects R6] YouTube channel disambiguation strategy — how to construct the search query to find the right channel. "Che rapper Atlanta" vs "Che music" vs just "Che".
- [Affects R11] What birth_year + N threshold to use for the release year floor. 12 (age 12) seems conservative. Could be 14 or 16.
- [Affects R12] Genre mismatch — which genre taxonomy to use (Discogs genres? MB tags?) and what threshold constitutes "strong conflict" vs normal genre breadth.
- [Affects R8] Whether Genius should move up in the chain (it has artist images but wasn't tested for "Che" — might work).

---

## Research Sources

- [Spotify "Which Witch" paper (2023)](https://research.atspotify.com/2023/11/which-witch-artist-name-disambiguation-and-catalog-curation-using-audio-and-metadata) — ML approach, 45% precision, confirms difficulty
- [Discogs API](https://www.discogs.com/developers) — 60 req/min, OAuth 1.0, community-curated
- [YouTube Data API v3](https://developers.google.com/youtube/v3/docs/channels) — 10K free units/day, channel thumbnails
- [AcoustID Web Service](https://acoustid.org/webservice) — fingerprint→MBID, 3 req/sec, free
- [MusicBrainz Release Group Search](https://musicbrainz.org/doc/MusicBrainz_API/Search/ReleaseGroupSearch) — Lucene query syntax for text search
- [SerpAPI Google Images](https://serpapi.com/images-results) — $25/month fallback option for future
- [Deezer artist conflation (community)](https://en.deezercommunity.com/deezer-catalogue-45/problem-with-2-artists-music-getting-mixed-up-because-they-have-the-same-name-30860) — confirms unfixed platform issue
