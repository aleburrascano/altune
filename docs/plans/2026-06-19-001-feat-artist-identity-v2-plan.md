---
title: "feat: Artist identity v2 — Discogs + YouTube artwork, optimistic discography filtering"
type: feat
status: active
date: 2026-06-19
origin: docs/brainstorms/2026-06-19-artist-identity-v2-requirements.md
---

# feat: Artist identity v2 — Discogs + YouTube artwork, optimistic discography filtering

## Summary

Add Discogs and YouTube Data API as new provider adapters to fix artwork and discography quality for same-name artists. Reorder the artwork chain to prioritize identity-verified sources. Replace the current "confirmed vs unconfirmed" discography ordering with optimistic include + heuristic removal (release year floor, genre mismatch, cross-reference). Define an AudioFingerprinter port for future AcoustID integration.

---

## Problem Frame

The detail screen for niche same-name artists shows wrong artwork (TheAudioDB returns a different artist's image) and contaminated discographies (Deezer conflates multiple real-world artists under one ID). All five current artwork resolvers fail for the test artist "Che". The discography filter wrongly keeps contamination visible and misclassifies real uncatalogued albums. (see origin: `docs/brainstorms/2026-06-19-artist-identity-v2-requirements.md`)

---

## Requirements

- R1. Discogs artwork resolver in the enrichment chain
- R2. Deezer→Discogs artist ID resolution via album-title overlap
- R3. Discogs ID resolution cache (Redis, 30-day TTL)
- R4. Discogs genre/year/country data for discography heuristics
- R5. YouTube artwork resolver (channel thumbnail via search)
- R6. YouTube channel disambiguation using MB disambiguation text
- R7. YouTube quota respect (cache 30-day TTL)
- R8. Artwork chain reordering: Fanart.tv → Discogs → YouTube → Genius → Deezer → TheAudioDB
- R9. TheAudioDB demoted to last
- R10. Optimistic include: keep albums unless positive mismatch evidence
- R11. Release year floor heuristic
- R12. Genre cluster mismatch heuristic
- R13. Discogs cross-reference for discography validation
- R14. MB text search fallback for uncatalogued albums
- R15. AudioFingerprinter port interface (no implementation)
- R16. Fingerprinting integration point in acquisition pipeline (deferred)
- R17-R19. Extensibility: new sources require one adapter + one wiring line

**Origin acceptance examples:** AE1 (artwork), AE2 (discography), AE3 (genre diversity), AE4 (extensibility), AE5 (fingerprinting port)

---

## Scope Boundaries

- SerpAPI, SoundCloud, Spotify, Google CSE, Bing — out of scope (see origin: Scope Boundaries)
- ML-based audio similarity — deferred, not rejected
- AcoustID implementation — deferred until acquisition pipeline matures
- User-uploaded artist images — deferred

### Deferred to Follow-Up Work

- Frontend rendering of `collapsed_artists` expansion UI (P4 from previous session)
- Frontend visual distinction between confirmed/unconfirmed albums (if optimistic-include still leaves edge cases)

---

## Context & Research

### Relevant Code and Patterns

- Artwork resolver pattern: `services/go-api/internal/discovery/adapters/providers/fanarttv.go` — constructor takes `(client, apiKey)`, implements `ports.ArtworkResolver`, returns `("", nil)` on failure
- Artwork chaining: `services/go-api/internal/discovery/adapters/providers/artwork_chain.go` — `ChainedArtworkResolver` tries resolvers in order
- Identity resolution pattern: `services/go-api/internal/discovery/adapters/providers/musicbrainz.go` — `ResolveArtistIdentity`, `ValidateArtistAlbums`, album-title cross-referencing
- Cache pattern: `services/go-api/internal/discovery/adapters/cache/mbid_cache.go` — wraps resolver with Redis, sentinel `"__none__"` for negative cache
- Provider wiring: `services/go-api/internal/app/app.go` lines 147-222 — conditional provider creation, config predicates
- Config: `services/go-api/internal/shared/config/config.go` — env var fields with `Has*()` predicates
- Discography filtering: `services/go-api/internal/discovery/service/get_artist_content.go` — `GetAlbums` with optional `AlbumValidator`
- Rate limiting: `services/go-api/internal/discovery/adapters/providers/musicbrainz.go` — mutex-based 1 req/sec enforcement (reuse for Discogs)

### External References

- Discogs API: personal token auth (`Authorization: Discogs token=<token>`), 60 req/min, search at `/database/search?type=artist&q=`, artist at `/artists/{id}`, releases at `/artists/{id}/releases`
- YouTube Data API v3: API key auth, search at `GET /youtube/v3/search?part=snippet&type=channel&q=`, 100 units/day quota for search, channel thumbnails via `GET /youtube/v3/channels?part=snippet&id=`

---

## Key Technical Decisions

- **Discogs personal token over OAuth**: simpler auth flow for a single-user backend. Token set via env var, sent as `Authorization: Discogs token=<TOKEN>` header. (see origin: Dependencies / Assumptions)
- **Discogs artist resolution via album-title overlap**: same approach as MB resolution in `ValidateArtistAlbums`. Search Discogs for artist name, get candidates, fetch each candidate's releases, pick the one with most album-title overlap against Deezer albums. Cache the resolved Discogs ID.
- **YouTube search uses disambiguation + genre context**: construct query as `"{name}" "{disambiguation}"` (e.g., `"Che" "Atlanta rapper"`). When no disambiguation exists, fall back to `"{name}" music`. Channel search, not video search — lower quota cost and returns the artist's profile thumbnail.
- **Release year floor threshold: birth_year + 14**: conservative enough to avoid false positives (14-year-olds release music) while killing clear contamination (albums from decades before the artist was born). When birth year is unknown, this heuristic simply doesn't fire.
- **Genre taxonomy: Discogs genres**: Discogs has a structured genre taxonomy (Hip Hop, Rock, Electronic, etc.) that's more reliable than MB's free-form tags. When Discogs genre data isn't available, genre heuristic doesn't fire.
- **2+ mismatch signals required for removal**: no single heuristic can remove an album alone. Require at least two of: year floor violation, genre mismatch, absent from both MB and Discogs. This prevents false positives from incomplete data.
- **Shared rate limiter for Discogs**: same mutex-based approach as MusicBrainz. Discogs rate limit is 60 req/min (not 1 req/sec), so the limiter enforces 1 second between requests to stay well within limits.
- **New `ProviderDiscogs` and `ProviderYouTube` enum values**: added to `domain/types.go` with `String()` and `ParseProviderName()` support.

---

## Open Questions

### Resolved During Planning

- [R2] How to resolve Deezer→Discogs artist ID: album-title overlap, same as MB. Search Discogs by name, fetch candidate releases, score by title overlap.
- [R6] YouTube channel search query construction: `"{name}" "{disambiguation}"`, fall back to `"{name}" music`.
- [R11] Release year floor threshold: birth_year + 14.
- [R12] Genre taxonomy: Discogs genres (structured, not free-form).

### Deferred to Implementation

- [R14] MB text search Lucene query syntax — exact query construction for `artist:X releasegroup:Y` needs runtime testing against MB API.
- [R12] What constitutes "strong conflict" between genre clusters — likely needs empirical tuning (hip-hop vs rock = clear conflict; hip-hop vs R&B = ambiguous).

---

## Implementation Units

- U1. **Domain types: add Discogs and YouTube provider names**

**Goal:** Register the two new providers in the domain enum so adapters can reference them.

**Requirements:** R17 (extensibility)

**Dependencies:** None

**Files:**
- Modify: `services/go-api/internal/discovery/domain/types.go`
- Modify: `services/go-api/internal/discovery/domain/types_test.go`

**Approach:**
- Add `ProviderDiscogs` and `ProviderYouTube` to the `ProviderName` iota enum
- Update `String()` and `ParseProviderName()` switch cases
- Add round-trip test cases

**Patterns to follow:**
- Existing provider enum pattern in `types.go` (ProviderDeezer, ProviderMusicBrainz, etc.)

**Test scenarios:**
- Happy path: `ParseProviderName("discogs")` returns `ProviderDiscogs`; `ProviderDiscogs.String()` returns `"discogs"`. Same for YouTube.
- Edge case: round-trip parse→string→parse for both new providers

**Verification:** `go test ./internal/discovery/domain/... -count=1` passes

---

- U2. **Config: add Discogs and YouTube API keys**

**Goal:** Make Discogs personal token and YouTube API key configurable via env vars.

**Requirements:** R1, R5 (provider setup)

**Dependencies:** None

**Files:**
- Modify: `services/go-api/internal/shared/config/config.go`
- Modify: `services/go-api/.env.example`

**Approach:**
- Add `DiscogsToken string \`env:"DISCOGS_TOKEN"\`` and `YouTubeAPIKey string \`env:"YOUTUBE_API_KEY"\`` fields
- Add `HasDiscogs()` and `HasYouTube()` predicates
- Add to `LogValue()` redacted output

**Patterns to follow:**
- `FanartTVAPIKey` / `HasFanartTV()` pattern in config.go

**Test expectation:** none — config field additions are structural, verified by build + integration

**Verification:** Build succeeds; `.env.example` documents the new vars

---

- U3. **Discogs adapter: artwork resolver + artist identity resolution**

**Goal:** Implement a Discogs adapter that resolves artist images and provides identity resolution (Discogs artist ID + genre/year data) for discography validation.

**Requirements:** R1, R2, R3, R4, R13

**Dependencies:** U1, U2

**Files:**
- Create: `services/go-api/internal/discovery/adapters/providers/discogs.go`
- Create: `services/go-api/internal/discovery/adapters/providers/discogs_test.go`

**Approach:**
- Constructor: `NewDiscogsAdapter(client *http.Client, token string)`
- Implement `ports.ArtworkResolver`:
  - `Resolve(ctx, kind, title, subtitle, mbid)` → search Discogs for artist by name, resolve to Discogs ID (via album overlap if needed), fetch artist images, return first image URL
  - Rate limit: mutex-based, 1 req/sec (conservative, within 60 req/min limit)
- Implement artist identity resolution (new method, not the port — used by discography heuristics):
  - `ResolveDiscogsArtist(ctx, name, albums) → (discogsID, genre, country, error)`
  - Search `/database/search?type=artist&q={name}`, get candidates
  - For each candidate, fetch `/artists/{id}/releases`, score by title overlap against provided albums
  - Return best match's ID + genre + country
- Auth header: `Authorization: Discogs token={token}`
- User-Agent header required by Discogs TOS

**Patterns to follow:**
- `fanarttv.go` for artwork resolver shape (return `"", nil` on failure)
- `musicbrainz.go` for rate limiting (mutex + time.Sleep) and identity resolution pattern

**Test scenarios:**
- Happy path: `Resolve` returns image URL when Discogs has the artist with images
- Happy path: `ResolveDiscogsArtist` picks the correct "Che" entry using album-title overlap
- Edge case: artist has no images on Discogs → returns `"", nil`
- Edge case: Discogs search returns zero candidates → returns `"", nil`
- Edge case: multiple same-name candidates, none with album overlap → returns first candidate
- Error path: Discogs API returns 429 (rate limited) → returns `"", nil`, logs warning
- Error path: Discogs API timeout → returns `"", nil`

**Verification:** Unit tests pass; manual test with "Che" returns Discogs image (if available)

---

- U4. **YouTube adapter: artwork resolver**

**Goal:** Implement a YouTube adapter that resolves artist profile images from YouTube channel thumbnails.

**Requirements:** R5, R6, R7

**Dependencies:** U1, U2

**Files:**
- Create: `services/go-api/internal/discovery/adapters/providers/youtube.go`
- Create: `services/go-api/internal/discovery/adapters/providers/youtube_test.go`

**Approach:**
- Constructor: `NewYouTubeArtworkResolver(client *http.Client, apiKey string)`
- Implement `ports.ArtworkResolver`:
  - `Resolve(ctx, kind, title, subtitle, mbid)` → only fires for `ResultKindArtist`
  - Search: `GET /youtube/v3/search?part=snippet&type=channel&q={query}&maxResults=1&key={key}`
  - Query construction: if subtitle (disambiguation) is present, use `"{title}" "{subtitle}"`; else use `"{title}" music`
  - If search returns a channel, fetch thumbnail: `GET /youtube/v3/channels?part=snippet&id={channelId}&key={key}`
  - Return highest-res thumbnail URL (high > medium > default)
- Quota: search costs 100 units, channels costs 1 unit. Total: 101 units per lookup. 10K units/day ≈ 99 lookups/day.

**Patterns to follow:**
- `fanarttv.go` for artwork resolver shape
- `genius.go` for API-key-based auth pattern

**Test scenarios:**
- Happy path: `Resolve` for artist kind returns channel thumbnail URL
- Happy path: disambiguation text used in query → finds correct channel
- Edge case: non-artist kind (track, album) → returns `"", nil` immediately
- Edge case: YouTube search returns zero results → returns `"", nil`
- Edge case: channel has no high-res thumbnail → falls back to medium/default
- Error path: API key invalid → returns `"", nil`, logs warning

**Verification:** Unit tests pass; manual test with "Che Atlanta rapper" finds the correct YouTube channel

---

- U5. **Discogs ID cache**

**Goal:** Cache Discogs artist ID resolution to avoid repeated lookups for the same artist.

**Requirements:** R3

**Dependencies:** U3

**Files:**
- Create: `services/go-api/internal/discovery/adapters/cache/discogs_cache.go`

**Approach:**
- Same pattern as `mbid_cache.go`: wrap `ResolveDiscogsArtist` with Redis caching
- Key: `discovery:discogs:v1:{sha256(normalized_name)[:16]}`
- TTL: 30 days positive, 24 hours negative
- Sentinel: `"__none__"` for "no Discogs match found"
- Cache the Discogs artist ID (string), not the full response

**Patterns to follow:**
- `mbid_cache.go` — wrapping pattern with sentinel

**Test expectation:** none — mirrors established cache pattern with no behavioral novelty

**Verification:** Build succeeds; integration test via manual API call confirms cache hit on second request

---

- U6. **Artwork chain reordering**

**Goal:** Reorder the artwork resolution chain so identity-verified sources fire first and name-only sources fire last.

**Requirements:** R8, R9

**Dependencies:** U3, U4

**Files:**
- Modify: `services/go-api/internal/app/app.go`

**Approach:**
- Current chain: Deezer → TheAudioDB → iTunes (in `artworkChain`), then Fanart.tv and Genius added as separate resolvers
- New chain order: Fanart.tv → Discogs → YouTube → Genius → Deezer → TheAudioDB → iTunes
- Move Fanart.tv INTO the chain (currently separate), add Discogs and YouTube
- TheAudioDB and iTunes move to last (name-based, no disambiguation)
- The `enrichOne` method in `search_music.go` already tries `fanartResolver` first, then `geniusResolver`, then the chain. Simplify: put ALL resolvers into the chain in the correct order, remove the separate resolver fields.

**Patterns to follow:**
- Existing `ChainedArtworkResolver` wiring in `app.go`

**Test scenarios:**
- Integration: search "Che" → artwork comes from Discogs or YouTube, not TheAudioDB
- Integration: search "Drake" → artwork still works (regression check, likely Deezer or Fanart.tv)

**Verification:** Manual test confirms "Che" gets correct artwork; all existing tests pass

---

- U7. **Discography heuristic filtering**

**Goal:** Replace "confirmed vs unconfirmed" ordering with optimistic include + heuristic removal. Albums are kept by default; contamination from other artists is removed when 2+ mismatch signals agree.

**Requirements:** R10, R11, R12, R13, R14

**Dependencies:** U3 (for Discogs genre/year data)

**Files:**
- Modify: `services/go-api/internal/discovery/service/get_artist_content.go`
- Modify: `services/go-api/internal/discovery/ports/ports.go`
- Create: `services/go-api/internal/discovery/service/discography_filter.go`
- Create: `services/go-api/internal/discovery/service/discography_filter_test.go`

**Approach:**
- New `filterContamination(albums, identity, discogsData) → filtered` function in a new file
- Input signals:
  - `identity.BirthYear` (from MB artist data, may be 0 if unknown)
  - `identity.DiscogsGenre` (from Discogs, may be empty)
  - `confirmed` map (album titles confirmed by MB and/or Discogs)
- Per album, compute mismatch score:
  - +1 if album year < birth_year + 14 AND birth_year > 0
  - +1 if album genre strongly conflicts with artist genre AND both are known
  - +1 if album is absent from both MB and Discogs confirmed lists
- Remove album if mismatch score >= 2
- Keep album if mismatch score < 2 (optimistic include)
- Extend `ArtistIdentity` port type with optional `BirthYear int` field
- Update `MusicBrainzAdapter.ResolveArtistIdentity` to extract birth year from MB response

**Patterns to follow:**
- `dedupAlbums` in `get_artist_content.go` for filtering shape
- `ValidateArtistAlbums` in `musicbrainz.go` for cross-reference pattern

**Test scenarios:**
- Covers AE2. Happy path: Che's albums — REST IN BASS (confirmed MB) kept, Samsonite (year mismatch + absent from MB/Discogs) removed, REST IN BASS: ENCORE (absent from MB but no year/genre mismatch) kept
- Happy path: mainstream artist (Drake) — all albums kept, no filtering applied (all confirmed)
- Edge case: artist with unknown birth year — year floor heuristic doesn't fire, only genre + cross-reference used
- Edge case: album with unknown year — year floor heuristic doesn't fire for that album
- Edge case: single mismatch signal (year only) — album kept (need 2+)
- Edge case: album absent from MB and Discogs but matching genre and year — kept (optimistic)
- Covers AE3. Integration: "Aurora" discography — Norwegian pop albums kept, no false removals

**Verification:** Unit tests pass with test cases covering each heuristic independently and in combination

---

- U8. **AudioFingerprinter port definition**

**Goal:** Define the port interface for future AcoustID integration without implementing it.

**Requirements:** R15, R16

**Dependencies:** None

**Files:**
- Modify: `services/go-api/internal/discovery/ports/ports.go`

**Approach:**
- Add `AudioFingerprinter` interface:
  ```
  VerifyArtist(ctx, audioData []byte) → (mbid string, confidence float64, error)
  ```
- No implementation, no wiring. Pure interface definition.

**Patterns to follow:**
- `AlbumValidator` interface shape in `ports.go`

**Test expectation:** none — interface-only, no runtime behavior

**Verification:** Build succeeds; interface exists in ports.go

---

- U9. **Wiring: connect new providers in app.go**

**Goal:** Wire Discogs and YouTube adapters into the application, conditioned on config.

**Requirements:** R17, R18, R19

**Dependencies:** U2, U3, U4, U5, U6, U7

**Files:**
- Modify: `services/go-api/internal/app/app.go`

**Approach:**
- In `setup()`:
  - If `HasDiscogs()`: create `DiscogsAdapter`, add to artwork chain, pass to discography filter
  - If `HasYouTube()`: create `YouTubeArtworkResolver`, add to artwork chain
  - Share single adapter instances (same pattern as `sharedMB`)
- Artwork chain construction: build resolver list in order, pass to `NewChainedArtworkResolver`
- Discography: pass Discogs adapter to `GetArtistContentService` via new option `WithDiscogsValidator`

**Patterns to follow:**
- `sharedMB` wiring pattern in app.go (single instance, shared between search and detail)
- `WithAlbumValidator` option pattern

**Test scenarios:**
- Covers AE4. Integration: adding a future provider requires one adapter file + one wiring line + config field
- Integration: app starts correctly with Discogs/YouTube keys missing (graceful degradation)
- Integration: app starts correctly with keys present (providers active)

**Verification:** `go build ./...` succeeds; app starts with and without the new API keys

---

## System-Wide Impact

- **Artwork chain**: resolvers are reordered. Existing resolvers (Fanart.tv, Genius, Deezer, TheAudioDB, iTunes) keep working but in new positions. TheAudioDB moves from 2nd to last.
- **Discography filtering**: `GetAlbums` behavior changes from "confirmed first, unconfirmed last" to "keep or remove". Frontend receives fewer albums (contamination removed). The `DiscographySections` component renders whatever it receives — no frontend changes needed.
- **Rate limiting**: shared MB adapter already enforces 1 req/sec. Discogs adds its own rate limiter. YouTube has no explicit rate limiter (quota is daily, not per-second).
- **New env vars**: `DISCOGS_TOKEN` and `YOUTUBE_API_KEY` need to be set in production `.env` and in the OCI deployment config.
- **Redis key namespace**: new cache keys `discovery:discogs:v1:*` added alongside existing `discovery:artwork:v1:*` and `discovery:mbid:*`.
- **Unchanged invariants**: search ranking, result merging, artist disambiguation, and collapse are unaffected. The new providers only affect artwork enrichment and detail-screen discography.

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Discogs may not have images for very niche artists | YouTube fallback covers this; worst case is album art fallback (existing) |
| YouTube channel search may return wrong channel for common names | Disambiguation text + genre context in query; cache means wrong result is correctable (clear cache, retry) |
| Discogs rate limit hit under load | Conservative 1 req/sec limiter; aggressive caching (30-day TTL) |
| Genre mismatch heuristic too aggressive | Require 2+ signals; genre alone never removes an album |
| "REST IN BASS: ENCORE" incorrectly filtered | Optimistic include: absent from MB/Discogs is only +1 signal, needs second mismatch to remove |

---

## Sources & References

- **Origin document:** [docs/brainstorms/2026-06-19-artist-identity-v2-requirements.md](docs/brainstorms/2026-06-19-artist-identity-v2-requirements.md)
- Related: [docs/brainstorms/2026-06-18-discovery-detail-residuals.md](docs/brainstorms/2026-06-18-discovery-detail-residuals.md)
- Related: [docs/brainstorms/artist-detail-quality-requirements.md](docs/brainstorms/artist-detail-quality-requirements.md)
- Discogs API: https://www.discogs.com/developers
- YouTube Data API v3: https://developers.google.com/youtube/v3
- Spotify "Which Witch" paper: https://research.atspotify.com/2023/11/which-witch-artist-name-disambiguation-and-catalog-curation-using-audio-and-metadata
