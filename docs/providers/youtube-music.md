# YouTube Music maximization

> Status: ✅ audited — live-probed 2026-06-22 (status codes + real field dumps against
> `music.youtube.com/youtubei/v1/{search,next,browse,music/get_search_suggestions}` and the
> `yt3/lh3.googleusercontent.com` artwork CDNs).
> Built today: **search** (track/video/album/artist via the `raitonoberu/ytmusic` lib) and **artist
> content** (albums + top tracks, feeding the consensus engine + artist-detail union). As of
> 2026-06-22, the **keyless artist-artwork resolver** (cap 3, the headline) is built — the one
> artist-image source iTunes lacks, with no API key and no Data-API quota. Relatedness (song radio),
> listen-popularity (monthly listeners / subscribers), lyrics, and bio were all mapped and verified
> reachable but are **deferred or skipped** as duplicative of already-maximized providers (§5).

## 1. Why this provider matters

YouTube Music is the **widest catalog** we reach — it indexes the official mainstream *and* the
video/UGC long tail (live cuts, covers, remixes, leaks YT Music files as "videos") that even
SoundCloud doesn't fully cover. That breadth already feeds search. But for *metadata maximization*
specifically — against the already-maximized set (MusicBrainz = identity + genres + CAA/Fanart
artwork; Discogs = credits + styles; Deezer = lyrics + bpm + popularity primary; Last.fm = listen
popularity + relatedness + tags; iTunes = keyless hi-res album artwork) — most of what YouTube Music
carries is **duplicative**. Its **one genuinely non-duplicative contribution is artist artwork**:

- **It is the keyless, quota-free artist-image source.** iTunes — our highest-resolution keyless
  artwork source — carries **no artist images at all** (its `musicArtist` entity has no artwork).
  The only YouTube artist-art path we had was the **official YouTube Data API** resolver
  (`youtube.go`), which is **key-gated** (`YOUTUBE_API_KEY`) and **quota-crippled** (a `search.list`
  costs 100 of the 10k default daily units → ~100 lookups/day — unusable in production). The internal
  YouTube Music API returns **real, high-resolution artist photos with no key and no quota** (verified:
  Kendrick's artist image resizes to a 13.9MB master at `=s0`; 1000px ≈ 134KB). This is the cap that
  earned a build (§5.3).

Everything else it offers is owned better elsewhere: **lyrics** → Deezer (synced + plain; YT Music is
plain-only); **listen popularity** → Last.fm/Deezer; **relatedness** → Last.fm similar / SoundCloud
related; **bio** → Discogs/Last.fm. See §5 for why each is deferred or skipped.

## 2. Access model

- **Tier 1 — internal JSON API.** Base: `POST https://music.youtube.com/youtubei/v1/{endpoint}?key=…`.
  This is the backend the YouTube Music web player calls. Each request carries a JSON body with a
  `context.client` of `clientName: "WEB_REMIX"`, `clientVersion: "1.20220715.04.00"`, `hl`/`gl`.
- **Auth bootstrap — a public `key`.** The internal API is gated only by an API key shipped in the
  web player's JS bundle: `AIzaSyC9XL3ZjWddXya6X74dJoCTL-WEYFDNX30` (hardcoded in the
  `raitonoberu/ytmusic` lib's `constants.go`). No user auth for public reads — a logged-out browser
  does exactly this. Unlike SoundCloud's `client_id`, this key has been **stable since the lib was
  published (2024)** and did not need re-resolution this session; if it ever rotates, the bootstrap
  is the same shape (regex the player JS) — not implemented yet because it hasn't been needed.
- **Library — `github.com/raitonoberu/ytmusic`** (already a dependency). It wraps `search`, `next`
  (watch playlist), `browse` (lyrics), and `music/get_search_suggestions`. The adapter bounds the
  lib's no-timeout global HTTP client and wraps its context-unaware calls (`fetchYTMusic` /
  `nextWithContext`).
- **Rate limit.** Not header-exposed. Bursty/concurrent calls trip an **intermittent HTTP 403 whose
  body is HTML**, which surfaces downstream as a JSON parse error (`invalid character '<'`). Verified
  repeatedly this session: a single `search` after idle → 200; several back-to-back `search`/`browse`
  calls → 403 until spaced out (the `browse` probes needed up to 6 attempts at 1.5s spacing). The
  `search` adapter already retries twice (`SearchTimeout` 3s); **do not run concurrent YT Music calls**
  (same discipline as the SoundCloud `api-v2` limit).
- **ToS / reach.** Reverse-engineered internal API, **against ToS** — accepted for this project's
  self-hosted personal/family use, named explicitly (README doctrine; same posture as SoundCloud's
  `client_id` and Deezer's `pipe` lyrics). Public-only: private/unlisted never surfaces.

## 3. Entity model

YouTube Music has five surface entities; the catch is **video ≈ track**:

| YT Music entity | our `ResultKind` | mapping |
|---|---|---|
| `track` (`TrackItem`: `videoId`, `title`, `artists[]`, `album`, `duration`, `isExplicit`, `thumbnails[]`) | `track` | direct |
| `video` (`VideoItem`: `videoId`, `title`, `artists[]`, `views`, `duration`) | `track` | **the UGC/long-tail recordings YT Music classifies as videos** (`MUSIC_VIDEO_TYPE_OMV/UGC`) — mapped as tracks (Pattern-C coverage fix), else the exact recording is absent from the candidate set |
| `album` (`AlbumItem`: `browseId` = `MPREb_…`, `title`, `type`, `artists[]`, `year`, `isExplicit`) | `album` | `browseId` is the album key for `browse` |
| `artist` (`ArtistItem`: `browseId` = `UC…` (a YouTube **channel** id), `artist`, `shuffleId`, `radioId`, `thumbnails[]`) | `artist` | `browseId` keys `browse`; `radioId` seeds song radio |
| `playlist` (`PlaylistItem`: `browseId`, `title`, `author`, `itemCount`) | (future playlist surface) | not a `ResultKind` today |

Identity is YouTube's own ids (`videoId`, channel `UC…`, `MPREb_…`) — **no ISRC, no MBID**, so it
contributes nothing to the cross-provider identity bridge (it only takes name searches in).

**Thumbnails are URL-resizable** (the iTunes-style win): album/track art on `yt3.googleusercontent.com`,
artist art on `lh3.googleusercontent.com`, both ending `=w{N}-h{N}[-p]-l90-rj`. Rewriting the
`w{N}-h{N}` segment resizes; `=s0` returns the raw master. Verified pixels/bytes: album cover
544→59KB, 1200→305KB, master(`=s0`)≈2000px/340KB (between CAA's 1200 and iTunes' 3000); artist photo
120→7KB, 1000→134KB, master(`=s0`) **13.9MB**.

## 4. Endpoint catalog (verified 2026-06-22)

| Endpoint | Returns | HTTP | Maps to |
|---|---|---|---|
| `POST /youtubei/v1/search` (+ `params` filter: Track/Album/Artist/Video/Playlist) | tracks, videos, albums, artists, playlists | 200 (intermittent 403/HTML) | `SearchProvider` ✅ built |
| `POST /youtubei/v1/next` (`videoId`, `playlistId: "RDAMVM"+videoId`) | **watch playlist / song radio** — 50 related tracks (verified: HUMBLE. → Not Like Us, SICKO MODE, Godzilla, Taste…) | 200 | relatedness — **deferred** (§5.4) |
| `POST /youtubei/v1/next` → lyrics `browseId` → `POST /youtubei/v1/browse` | **plain lyrics** text (verified: HUMBLE. → 3050 chars) | 200 | lyrics — **skipped** (§5.6) |
| `POST /youtubei/v1/browse` (`browseId: UC…`) | `musicImmersiveHeaderRenderer`: `monthlyListenerCount` ("168M monthly audience"), `description` (Wikipedia bio, CC-BY-SA), `subscriptionButton.longSubscriberCountText` ("20.2M subscribers"), resizable `thumbnails` | 200 | artist popularity/bio/artwork — artwork **built** (§5.3), rest **deferred/skipped** |
| `POST /youtubei/v1/browse` (`browseId: MPREb_…`) | `microformat`: album `description`, canonical playlist url, 544px resizable `thumbnail`; `contents.twoColumnBrowseResultsRenderer`: tracklist | 200 | album enrichment — **deferred** (§5.5) |
| `POST /youtubei/v1/music/get_search_suggestions` (`input`) | autocomplete strings (verified: "kendr" → "kendrick lamar", "kendrick", …) | 200 | search-UX — **deferred** (§5.7) |
| `GET https://{yt3,lh3}.googleusercontent.com/…=w{N}-h{N}…` | resolution-templated cover/photo (resizable; `=s0` = master) | 200 | **artist artwork ✅ built (§5.3)** |

## 5. Capabilities to maximize

### 1. Search (track / video / album / artist) — ✅ BUILT
`Search` dispatches the general `ytmusic.Search` and maps `result.Tracks` **and** `result.Videos`
(Pattern-C: obscure recordings YT Music files as videos) to tracks, plus albums and artists. Rich
`extras` (duration, album, year, record_type) carried; not wired into ranking (coverage, not ranking).
Code: `ytmusic.go` (`mapYTMusicTrack` / `mapYTMusicVideo` / `mapYTMusicAlbum` / `mapYTMusicArtist`).

### 2. Artist content (discography + top tracks) — ✅ BUILT
`GetArtistAlbums` (`AlbumSearch`, artist-name filtered) and `GetArtistTopTracks` (`TrackSearch`,
artist-name filtered, capped at 10) implement the content ports. Wired as `"ytmusic"` in the
consensus providers (`search_wiring.go`) and the artist-content dispatch. Off the ranking path; the
album set feeds the MB-validated consensus union alongside Deezer/iTunes/SoundCloud.

### 3. Keyless artist artwork (internal API, hi-res resize) — ✅ BUILT (2026-06-22, the headline)
The single non-duplicative metadata win. `YouTubeMusicArtworkResolver.Resolve` (artist-only) runs an
`ArtistSearch`, picks the best artist image (`pickArtistArtwork`: exact case-insensitive name match
preferred, top result as fallback), and rewrites it to a **1000px hero** (`resizeYTThumbnail`,
preserving the `-p-` smart-crop flag). Returns `""` on miss so the chain falls through. Wired in
`buildArtworkChain` **after** the ID-keyed sources and **before** the key-gated official-API
`YouTubeArtworkResolver` — which is now a deeper fallback (kept, not deleted; largely redundant).
**Why it earns a build where lyrics/popularity/bio don't:** iTunes (our best keyless artwork source)
has no artist images, Fanart.tv is MBID-gated, and the official Data-API resolver is quota-crippled
(~100 lookups/day) — so for an artist with no MBID and no Fanart/Deezer/Discogs hit, this is the only
keyless hi-res artist photo we can get. **Display-only, off the ranking path, no eval gate** — mirrors
the iTunes/Deezer/SoundCloud artwork-fallback pattern exactly. Album/track art is **deliberately
excluded**: the ID-keyed sources (CAA 1200 / Deezer 1000 / iTunes 1500-from-3000) already cover it,
and YT Music's ~2000px album master adds no ceiling above iTunes.

### 4. Song radio / watch-playlist relatedness — ⬜ DEFERRED (feature-spec)
`ytmusic.GetWatchPlaylist(videoID)` (`next` with `playlistId: "RDAMVM"+videoId`) returns **50 related
tracks** — a real mainstream relatedness graph (verified). But it is the *mainstream* counterpart to
two surfaces we already have: SoundCloud's underground `RelatedTracksProvider` (built) and Last.fm
`track.getSimilar` (deferred). The surface decision — a "Related on YouTube" rail vs a unified
related set across providers — is a `/feature-spec`, deliberately deferred exactly as the SoundCloud
related-tracks and Last.fm similar-tracks rails were. The adapter method is then trivial (one lib call,
reuse `mapYTMusicTrack`).

### 5. Album enrichment (description + tracklist via `browse`) — ⬜ DEFERRED (duplicative)
Album `browse` (`MPREb_…`) yields a Wikipedia-style `description` (microformat) and the full
tracklist. Duplicative: descriptions are owned by Discogs/Last.fm/Deezer enrichment; tracklists by
the Deezer/iTunes/MB content ports. Low value; defer unless a YT-exclusive album axis appears.

### 6. Lyrics (plain) — ❌ SKIPPED (Deezer owns it, superior)
`ytmusic.GetLyrics(videoID)` returns **plain** lyrics (verified: 3050 chars). Deezer already provides
**synced + plain** lyrics with writers + copyright (the headline of that provider). YT Music's plain
text is strictly inferior — no sync, no credits. Do not wire.

### 7. Listen popularity + bio (monthly listeners / subscribers / Wikipedia bio) — ⬜ DEFERRED (duplicative)
Artist `browse` carries `monthlyListenerCount` ("168M monthly audience"), `subscriberCount` ("20.2M
subscribers"), and a CC-BY-SA Wikipedia `description`. Listen popularity is owned by Last.fm
(`listeners`/`playcount`) and Deezer (`nb_fan`/`rank`, the ranking primary); bio by Discogs/Last.fm.
A YT-monthly-listeners signal *into rank* would be eval-gated and is unlikely to beat Deezer — defer.

### 8. Search suggestions / autocomplete — ⬜ DEFERRED (search-UX, not metadata)
`ytmusic.GetSearchSuggestions(input)` returns ranked autocomplete (verified: "kendr" → "kendrick
lamar", …). A search-box UX feature, not entity metadata — out of scope for metadata maximization;
needs a `/feature-spec` if a typeahead surface is ever wanted.

## 6. Costs & risks

- **The intermittent HTML-403 rate-limit is the real cost.** Not header-exposed; bursty/concurrent
  calls trip it (body is HTML → `'<'` JSON error). Mitigations: the `search` adapter retries twice;
  the artwork resolver inherits `fetchYTMusic`'s single-flight + context-aware retry; **never run
  concurrent YT Music calls** (don't run evals against it concurrently). A per-result artwork lookup
  on detail-open is fine; **no blocking YT Music call on the hot search path** beyond the existing
  bounded search fan-out.
- **Public-key stability.** The `WEB_REMIX` key is hardcoded in the lib and has been stable since
  2024. If it rotates, the lib (and thus search + content + artwork) breaks until the dep is bumped or
  a key-resolver is added (regex the player JS, like SoundCloud's `client_id`). Not implemented —
  not yet needed.
- **No ISRC / no MBID — name-match risk.** Artwork resolves by name search; `pickArtistArtwork`
  prefers an exact case-insensitive name match to cut wrong-artist images, and the resolver sits
  **after** the ID-keyed sources so it only fires when they miss (same posture as the iTunes/Deezer
  fallbacks). A name-matched artist photo beats no photo.
- **ToS.** Reverse-engineered internal API, grey area — accepted for self-hosted use, named
  explicitly. Public-only reach.
- **Not an album-artwork upgrade, not a popularity/lyrics/relatedness primary.** ~2000px album master
  < iTunes 3000; lyrics plain-only < Deezer synced; popularity duplicates Last.fm/Deezer. Don't
  re-task it — its unique value is **artist images** (§5.3).
- **Caching (artwork follow-on).** Like iTunes, an artwork resolve costs a (rate-limited) search; a
  read-through name-keyed cache (long positive TTL — artist photos are static) is the natural
  follow-on if YT-fallback resolves become frequent. Not blocking; the resolver runs late in the chain.

## 7. Current implementation state

Search + content (the original projection) and the new keyless artist-artwork resolver:

- `services/go-api/internal/discovery/adapters/providers/ytmusic.go` — `YouTubeMusicAdapter`
  (`Search` over track/video/album/artist; `GetArtistAlbums` / `GetArtistTopTracks`; the
  `fetchYTMusic` retry + `nextWithContext` context-bridge; mappers) **and**
  `YouTubeMusicArtworkResolver` (`Resolve` artist-only; `pickArtistArtwork` / `largestYTThumbnail` /
  `resizeYTThumbnail` pure helpers; `ytArtworkHeroSize` = 1000).
- `services/go-api/internal/discovery/adapters/providers/youtube.go` — the **official YouTube Data
  API** `YouTubeArtworkResolver` (artist channel thumbnails), key-gated by `cfg.HasYouTube()`. Now a
  deeper fallback behind the keyless resolver; **retained, not deleted** (still the only path that
  uses the official API; trivially removable if the keyless resolver proves sufficient on live data).
- Wired in `internal/app/search_wiring.go`: `NewYouTubeMusicAdapter` as a consensus provider + the
  artist-content dispatch; `NewYouTubeMusicArtworkResolver()` in `buildArtworkChain` after iTunes,
  before the key-gated official resolver. `internal/app/app.go` adds the search adapter to the
  provider list. The search adapter is **unconditionally wired** (the internal API needs no key).
- Tests: `ytmusic_test.go` covers the context-cancel guard, `mapYTMusicVideo`, and the new pure
  artwork helpers (`resizeYTThumbnail` table, `pickArtistArtwork` exact/fallback/empty cases, and the
  artist-only no-op guard for non-artist kinds). Full `internal/discovery/...` suite green (600).

## 8. Next steps

Caps 1–3 are built — the **non-duplicative** maximization (artist artwork) is done. Everything else is
deferred-by-design or skipped (§5), not a coverage gap:

1. **Song radio relatedness (cap 4).** A "Related on YouTube" rail — needs a `/feature-spec` surface
   decision (rail vs unified related set across SoundCloud/Last.fm/YouTube), then a thin
   `GetWatchPlaylist` mapper. Deferred exactly like the SoundCloud/Last.fm related surfaces.
2. **Artwork cache (cap 3 follow-on).** Name-keyed read-through cache (long positive TTL) if
   YT-fallback artwork resolves become frequent — mirror `RedisEnrichmentCache`. Not blocking.
3. **Retire the official-API resolver (`youtube.go`).** Once the keyless resolver is confirmed
   sufficient on live data, the key-gated Data-API resolver and the `YOUTUBE_API_KEY` config can be
   removed. Kept for now (surgical change; no deletion of working code without live confirmation).
4. **Public-key self-heal.** Add a player-JS key resolver (like SoundCloud's `client_id`) **only if**
   the hardcoded `WEB_REMIX` key starts rotating. Not needed today.

**Not verifiable in this dev environment:** the real-world artist-artwork coverage lift on live
traffic (needs the running pipeline + a device). All §4 endpoints, the artwork-resolution ceiling,
and the rate-limit behavior were probed live this session via the `raitonoberu/ytmusic` lib and direct
`youtubei/v1` calls; the resolver's core logic is covered by pure unit tests.
