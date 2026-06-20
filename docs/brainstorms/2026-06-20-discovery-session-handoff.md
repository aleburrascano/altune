---
date: 2026-06-20
topic: discovery-pipeline-session-handoff
---

# Discovery Pipeline — Session Handoff

## What happened this session

A single marathon session that went: **audit → brainstorm → grill → plan → implement → test → iterate**. The discovery pipeline underwent a fundamental redesign.

## What was built (branch: `refactor/discovery-pipeline-clarity`)

### Identity resolution → Multi-provider consensus
- **Deleted**: identity_resolver.go, identity_constraints.go, identity_cache.go (~2,189 lines of heuristic code)
- **Added**: consensus.go — queries multiple providers for artist albums, merges into a union, classifies as confirmed (2+ providers) / unconfirmed (1 provider) / rejected
- **MB authority filter**: when MusicBrainz has 10+ confirmed titles for an artist, unconfirmed albums are rejected as likely contamination from same-name artists
- **Deezer search fallback**: when an album from consensus has no Deezer source, the backend searches Deezer by title+artist to find tracks

### New providers added
- **YouTube Music** (`raitonoberu/ytmusic` Go library) — search provider + consensus provider. No API key, no auth. Massive catalog.
- **Cover Art Archive** — MBID-keyed album art up to 1200px, no rate limit
- **Tidal** — adapter built but **client_credentials not supported** by Tidal's developer portal for third-party apps. Code stays but is inactive.

### Provider expansions
- **Last.fm** — added `GetArtistAlbums` (`artist.getTopAlbums`) and `GetArtistTopTracks` with play counts
- **Wikidata** — added `ResolveByDeezerID` for direct ID→MBID resolution

### Other changes
- **Artwork chain**: restructured to ID-first (Cover Art Archive → Fanart.tv → name-search fallback)
- **RRF weights**: equalized to 1.0 for all providers
- **Click signals**: wired ClickSignalProvider to persistence + ranking boost
- **Diagnostic logging**: per-stage `pipeline.*` logs at debug level
- **Album limit**: raised from 10 to 50
- **Nil slices**: fixed in all error responses
- **Dedup key**: includes artist name, not just title
- **Duplicate key fix**: mobile DiscographySections uses title+sourceID key

## Test results (as of end of session)

| Artist | Albums | Status | Notes |
|---|---|---|---|
| **OsamaSon** | 41 confirmed | ✓ Working | Was 2 albums before. Deezer search fallback enables track loading for non-Deezer albums. Some albums still can't load tracks if Deezer doesn't have them at all. |
| **Killeastsxde** | 27 (8 confirmed, 19 unconfirmed) | ✓ Working | MB has no data → no authority filter, all kept |
| **Che** | 48 confirmed | ✓ Working | Was 100+ with contamination. MB authority filter cleaned it |

## Known issues / not perfect yet

1. **Some OsamaSon albums still can't load tracks** — the Deezer search fallback finds Deezer matches for some albums but not all. Albums that Deezer truly doesn't have return empty tracklists.

2. **Consensus latency** — detail page takes 5-12 seconds because it queries 5 consensus providers in parallel + MB validation. Discogs is the slowest (artist resolution + release fetch). Could be improved with caching.

3. **Che still has some contamination** — the user noted "a little contaminated" in the 48 confirmed albums. The MB authority filter is good but not perfect. The consensus confirmation threshold (2+ providers) may be too permissive for common names.

4. **YouTube Music search returns 0 results** — the `Search()` method returns tracks/albums/artists in the general search but they're mapping to 0 results in the search pipeline. The `AlbumSearch()` and `TrackSearch()` work fine for consensus. Needs investigation.

5. **Tidal unusable** — client_credentials grant type not available for third-party apps. The adapter is built and ready but can't authenticate.

6. **No top tracks for underground artists** — Killeastsxde and OsamaSon show 0 top tracks because Deezer has none. Could fetch top tracks from Last.fm or YouTube Music as fallback.

## Provider stack (current)

### Search providers (7)
Deezer, iTunes, TheAudioDB, MusicBrainz, Last.fm, SoundCloud, YouTube Music

### Consensus providers (5)
Last.fm, MusicBrainz, Discogs, iTunes, YouTube Music

### Artwork chain
Cover Art Archive (MBID) → Fanart.tv (MBID) → Genius → TheAudioDB → Deezer → iTunes → YouTube

## What's next: ML

The user wants to explore ML for the discovery pipeline. Context from the grilling:

**What ML could solve that deterministic consensus can't:**
- For common-name artists, ALL providers have contamination. Consensus confirms wrong albums because multiple providers agree on the wrong data. ML could learn "these albums don't belong to this artist" from patterns humans can see but rules keep getting wrong.
- Provider selection: which provider has the best data for which type of artist? ML could learn that for underground hip-hop, YouTube Music > Deezer; for classical, Discogs > Last.fm.

**What ML needs:**
1. Training data — the user has ~10 daily users. Click signals are being collected (wired this session). Add a "wrong album" feedback button for explicit negative labels.
2. Start simple: gradient-boosted tree (XGBoost) over features like provider count, genre consistency, popularity ratios, title similarity to confirmed albums.
3. The click signals + consensus status provide natural labels: confirmed albums = positive, rejected albums = negative, user clicks = behavioral positive.

**The user's vision:** "Instead of having hard-coded things in the code, the machine learning does everything." They want ML to replace all thresholds, genre maps, and hardcoded logic with a learned model.

**Practical path:** collect data → simple model → iterate. The infrastructure for data collection is already in place (click signals, consensus status, diagnostic logging).

## Key files

- `services/go-api/internal/discovery/service/consensus.go` — the consensus service
- `services/go-api/internal/discovery/service/get_artist_content.go` — artist detail with consensus integration
- `services/go-api/internal/discovery/service/get_album_tracks.go` — album tracks with Deezer search fallback
- `services/go-api/internal/discovery/adapters/providers/ytmusic.go` — YouTube Music adapter
- `services/go-api/internal/discovery/adapters/providers/coverartarchive.go` — Cover Art Archive
- `services/go-api/internal/discovery/adapters/providers/tidal.go` — Tidal (inactive)
- `services/go-api/internal/discovery/ARCHITECTURE.md` — pipeline diagrams (needs YouTube Music update)
- `services/go-api/internal/app/app.go` — DI wiring for all providers + consensus
- `docs/brainstorms/2026-06-19-discovery-pipeline-clarity-requirements.md` — requirements
- `docs/brainstorms/2026-06-20-provider-inventory.md` — full provider capability matrix
- `docs/plans/2026-06-20-001-refactor-discovery-pipeline-clarity-plan.md` — implementation plan

## Documents to read first in next session

1. This handoff doc
2. `docs/brainstorms/2026-06-20-provider-inventory.md` — knows every provider and what it can do
3. `services/go-api/internal/discovery/ARCHITECTURE.md` — pipeline flow diagrams
4. `services/go-api/internal/discovery/service/consensus.go` — the consensus logic
