---
date: 2026-06-20
topic: provider-inventory
---

# Provider Inventory — Discovery Pipeline

Complete inventory of all music metadata, artwork, and identity providers available to Altune's discovery pipeline. Compiled during the discovery pipeline clarity brainstorm (2026-06-19/20).

## Metadata Providers (search, albums, discography, top tracks)

| Provider | Search | Artist Albums | Top Tracks | ISRC | MBID | Popularity Signal | Status | Action |
|---|---|---|---|---|---|---|---|---|
| **Deezer** | track/album/artist | `/artist/{id}/albums` | `/artist/{id}/top` | Yes | No | rank, nb_fan | Integrated — primary | Keep |
| **MusicBrainz** | track/album/artist | release-groups by artist | No | Yes | Yes (source of truth) | No | Integrated — identity only | Expand to consensus |
| **Discogs** | No | `/artists/{id}/releases` | No | No | No | No | Integrated — identity only | Expand to consensus |
| **Last.fm** | track/album/artist | `artist.getTopAlbums` | `artist.getTopTracks` (with play counts) | No | Yes (returns MBIDs) | listeners (20+ yrs scrobble data) | Integrated — search + charts | Expand: add album listing + top tracks |
| **iTunes** | track/album/artist | search `entity=album&term={artist}` | No | No | No | No | Integrated — search + artwork | Keep — already consensus-capable |
| **SoundCloud** | tracks only (via yt-dlp) | No | No | No | No | playback_count | Integrated — search only | Keep as-is (tracks only) |
| **Tidal** | track/album/artist | artist albums API | artist top tracks | Yes | No | No | Integrated — inactive (client_credentials not available for 3rd party apps) | Adapter built, unusable without user auth |
| **YouTube Data API** | video/channel search | No (video-centric) | No (views, not tracks) | No | No | view counts | Integrated — artwork only | Keep for artwork |
| **YouTube Music** | track/album/artist | album search by artist | track search by artist | No | No | No | Integrated — search + consensus | No auth, massive catalog (raitonoberu/ytmusic Go lib) |

### Provider details

**Deezer** — Primary workhorse. Free, no auth for catalog reads. 50 req/5s rate limit. Returns ISRC on tracks, rank/nb_fan for popularity, genre_id on albums, preview URLs. Structured search (`artist:"X" track:"Y"`). Also provides charts for vocabulary building. ToS: non-commercial only.

**MusicBrainz** — Identity authority. Free, 1 req/sec rate limit. Returns MBIDs (canonical identifiers), ISRCs, artist disambiguation, area, type, tags. Release-groups provide authoritative discography. Currently used for album validation in identity resolver — repurpose for consensus. PostgreSQL dumps available for bulk/self-hosting.

**Discogs** — Physical release database. Free, 60 req/min authenticated. Returns genre, country, release year/type. Artist releases provide discography. Currently used for identity enrichment — repurpose for consensus. Weaker on digital-only/streaming-era releases.

**Last.fm** — Behavioral data powerhouse. Free, 5 req/sec. `artist.getTopAlbums` and `artist.getTopTracks` with real listener play counts (20+ years of scrobble data). Returns MBIDs. Currently only used for search and chart vocabulary — expand to album listing and top tracks. Strongest free popularity signal available.

**iTunes** — Free, no auth, no published rate limit. Search returns albums by artist name. `LookupAlbum` exists for cross-referencing. Returns genre (primaryGenreName), high-res artwork (upscaled), duration, preview URLs.

**SoundCloud** — No official API (deprecated 2019). Accessed via yt-dlp subprocess (`scsearch5:{query}`). Returns tracks only — no albums, no discography. Returns playback_count. Useful for SoundCloud-exclusive content (remixes, bootlegs). Cannot contribute to album consensus.

**Tidal** — NEW. Open developer portal at developer.tidal.com. OAuth 2.0 client credentials (free registration, no approval gate). Artist albums, top tracks, ISRC lookup (`GET /tracks?filter[isrc]=`). Well-structured metadata. Historically most developer-friendly streaming platform. ToS needs review but enforcement posture is lenient.

**YouTube Data API v3** — Free, 10,000 units/day (search costs 100 units = ~100 searches/day). Currently used only for artist channel thumbnail artwork. View counts could serve as a popularity signal during enrichment. No structured album/discography data (video-centric). ToS prohibits "substitute" apps but metadata reads are fine for non-commercial use.

## Artwork Providers

| Provider | Artist Images | Album Covers | Needs MBID? | Quality | Status | Action |
|---|---|---|---|---|---|---|
| **Deezer** | `picture_big` | `cover_big` | No (uses Deezer ID) | 500x500 | Integrated | Keep — always correct for that ID |
| **Fanart.tv** | HD thumbs, banners, logos | HD album art, CD art | Yes | High (fan-curated HD) | Integrated | Keep — best quality with MBID |
| **Genius** | artist images | song art | No (name search) | Medium | Integrated | Keep but demote — name search causes wrong artist |
| **TheAudioDB** | artist thumbs | album art | No (name or MBID) | Medium | Integrated | Keep for artwork (discography endpoint is paid-only) |
| **iTunes** | No | high-res covers (upscaled to 600px) | No (name search) | High | Integrated | Keep |
| **YouTube** | channel profile pics | No | No | Low-Medium | Integrated | Keep — last resort for artist photos |
| **Cover Art Archive** | No | 250/500/1200px | Yes | High (up to 1200px) | Not integrated | Add — free, no rate limit, best album art with MBIDs |
| **Discogs** | artist images | No | No | Medium | Integrated | Keep |

### Artwork chain recommendation

Priority order for artist images: Deezer own image (always correct for ID) → Fanart.tv (HD, needs MBID) → TheAudioDB → Genius (name search, last resort) → YouTube channel thumbnail.

Priority order for album covers: Deezer own cover → Cover Art Archive (HD, needs MBID) → Fanart.tv → iTunes (name search) → TheAudioDB.

Key principle: **ID-based artwork first, name-search artwork last.** Name-based search is what causes wrong-artist photos.

## Identity / Cross-Reference Providers

| Provider | Purpose | Status | Action |
|---|---|---|---|
| **Wikidata** | Deezer artist ID → MBID bridge (SPARQL) | Integrated — artwork only | Expand: use for search merging too |
| **AcoustID** | Audio fingerprint → MBID resolution | Not integrated | Consider later — relevant for import/acquisition, not discovery |
| **ListenBrainz** | Scrobble data, listening stats | Not integrated | Skip — Last.fm already covers this |

## Ruled-Out Providers

| Provider | Reason |
|---|---|
| **Spotify** | Feb 2026: requires Premium, client credentials being deprecated. ToS bans caching, cross-service data transfer, aggregating databases. |
| **Apple Music** | Requires $99/year Apple Developer Program membership. ToS bans competing services. |
| **SoundCloud (official API)** | Deprecated since 2019. Requires paid Artist Pro subscription for new apps. |
| **Napster** | Shut down January 2026. Service is dead. |
| **Amazon Music** | Closed beta since 2024. Rejecting all applications. No timeline. |
| **Bandcamp** | Seller-only API (sales reports). No catalog/search API. Scraping is ToS violation. |
| **JioSaavn** | No official API. Unofficial wrappers are explicit ToS violations. South Asian catalog only. |
| **Audiomack** | Free registration but no album objects (tracks/playlists only). Hip-hop/Afrobeats niche. |
| **Rate Your Music / Sonemic** | No public API. Beta never shipped. |
| **AllMusic** | No public API. Data is commercially licensed to partners only. |

## Actions Summary

**Adding (new adapters):**
- Tidal — artist albums, top tracks, ISRC lookup
- Cover Art Archive — MBID-keyed album art up to 1200px

**Expanding (existing adapters, new endpoints):**
- Last.fm — add `artist.getTopAlbums`, `artist.getTopTracks`
- YouTube — use view counts as popularity signal
- Wikidata — use Deezer→MBID bridge during search merging
- MusicBrainz — repurpose album validation for consensus (not filtering)
- Discogs — repurpose artist releases for consensus (not filtering)

**Keeping as-is:**
- Deezer, iTunes, SoundCloud, Fanart.tv, Genius, TheAudioDB
