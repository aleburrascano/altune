# SoundCloud maximization

> Status: ✅ audited — live-probed 2026-06-21 (status codes + real field dumps against `api-v2.soundcloud.com`).
> Built today: track search + `resolve`. The other five capabilities below are mapped and verified reachable, not yet wired.

## 1. Why this provider matters

SoundCloud is the **only** provider that carries the unreleased / leaked / underground long tail —
the exact tracks no mainstream catalog (Deezer, iTunes, MusicBrainz, Tidal) indexes. For a
self-hosted **owned-library** product, it is also the **only acquisition source** for that long
tail: the same API that finds an unreleased track also exposes its full audio stream. That makes
SoundCloud arguably the highest-leverage provider we have — not just another search source.

## 2. Access model

- **Tier 1 — internal JSON API.** Base: `https://api-v2.soundcloud.com`. This is the backend
  SoundCloud's own website calls; the public site is rendered *from* it, so scraping HTML would
  yield strictly less. **Do not scrape SoundCloud.**
- **Auth bootstrap — `client_id`.** The internal API is gated only by a `client_id` query param
  (no user auth for public reads — a logged-out browser does exactly this). The site embeds a
  public `client_id` in its JS bundles:
  1. `GET https://soundcloud.com/` → HTML
  2. regex the `…/assets/*.js` bundle URLs
  3. fetch bundles, regex `client_id:"<32 alnum>"`
  4. cache it; on `401/403` invalidate and re-resolve (the rotation tax)
  Implemented in `clientIDResolver` (singleflight-deduped, self-healing).
- **ToS / reach.** Reverse-engineered, against ToS — accepted for self-hosted personal/family use.
  Public-only: private/unlisted tracks never surface; publicly-uploaded leaks do.

## 3. Entity model (the key insight)

SoundCloud has **only three entity types**, and they don't line up 1:1 with our `ResultKind`:

| SoundCloud entity | our `ResultKind` | mapping |
|---|---|---|
| `track` | `track` | direct |
| `user` | `artist` | **every uploader is a "user"** — no separate artist type |
| `playlist` | `album` or playlist | a playlist with `set_type: "album"/"ep"/"single"` **is** an album (`is_album:true`) |

So all three kinds are reachable — you map `user → artist` and `album-playlist → album`.

## 4. Endpoint catalog (verified 2026-06-21)

| Endpoint | Returns | HTTP | Maps to |
|---|---|---|---|
| `GET /search/tracks?q=&limit=&offset=` | tracks (paginated via `next_href`) | 200 | `SearchProvider` (track) ✅ built |
| `GET /search/users?q=` | artists (users) | 200 | `SearchProvider` (artist) + `ArtworkResolver` |
| `GET /search/albums?q=` | albums/EPs/singles (typed playlists) | 200 | `SearchProvider` (album) + `ArtworkResolver` |
| `GET /search/playlists?q=` | playlists | 200 | (future playlist surface) |
| `GET /resolve?url=` | any permalink → entity | 200 | import / acquisition ✅ built |
| `GET /tracks/{id}/related?limit=` | related tracks (recommendations) | 200 | new related-tracks feature |
| `GET /charts?kind=trending&genre=soundcloud:genres:<g>` | trending by genre | 200 | `ChartProvider` |
| `GET /users/{id}/tracks · /toptracks · /albums · /playlists` | an artist's catalogue | (probe per id) | `ArtistContentProvider` |
| `GET /playlists/{id}` | album → its track list | (probe per id) | `AlbumContentProvider` |
| `GET /users/{urn}` (direct) | user detail | **401** with URN form | use numeric id / resolve instead |

## 5. The six capabilities to maximize

### 1. Track search — ✅ BUILT
`/search/tracks`, paginated to ~40 (vs yt-dlp's 5). Maps to `SearchProvider` (track). Rich `extras`
(genre, playback/likes/reposts) carried but **not** wired into ranking (coverage, not ranking).
Code: `soundcloud_apiv2.go`.

### 2. Album + artist search — ✅ BUILT
`SupportedKinds` now includes `album` (`/search/albums`, typed playlists) and `artist`
(`/search/users`). `Search` dispatches per kind via a shared `resolveAndFetch` client_id/auth-retry
helper; mappers for the album-playlist and user objects.
- **Album fields:** `artwork_url, set_type, is_album, track_count, release_date, description, genre, created_at, user`.
- **Artist (user) fields:** `avatar_url, username, full_name, city, country_code, followers_count, verified, creator_subscriptions[] (pro/badges), description, permalink_url`.
- **Risk:** ranking gate — SC "albums" are often bootlegs/compilations and SC "artists" are noisy
  (any uploader). Must run the full `discoveryeval --pipeline v2` gate (≥ baseline top-3) before/after,
  exactly as the track increment did.

### 3. Artwork resolver — ✅ BUILT
`SoundCloudAPIAdapter` implements `ports.ArtworkResolver` (`Resolve(kind,title,subtitle,mbid)`),
wired **last** in `buildArtworkChain` so it only fires for entities the ID-based sources miss — the
underground long tail where SoundCloud is the sole artwork source (track/album `artwork_url`, artist
`avatar_url`, all bumped to 500px). Returns `""` on miss so the chain falls through. The bespoke
permalink resolver was renamed `ResolvePermalink` to free the `Resolve` signature for this port.

### 4. Artist discography — planned
`/users/{id}/tracks`, `/toptracks`, `/albums` → populate an underground artist's page. Feeds
`ArtistContentProvider`. Needs a resolved numeric user id (from `/search/users` or `/resolve`).
**Low risk** (read-only enrichment, not the ranking path).

### 5. Related tracks (recommendations) — planned (own feature)
`/tracks/{id}/related` returns a recommendation set — live probe surfaced underground collabs
(e.g. "Lil Tecca & Ken Carson – Fell In Love") that pure search misses. This is a **new discovery
feature**, not coverage; deserves its own spec (how it surfaces in the UI, when it's called).

### 6. Audio acquisition — planned (separate thread)
The owned-library unlock. Each track carries `media.transcodings[]` — the stream URLs.
**It is the FULL track, not a 30s preview** (verified: `duration == full_duration`, `snipped:false`).
Acquisition difficulty varies by tier, *not* preview-vs-full:
- **Underground / leaks (our targets):** expose a **`progressive`** MP3 transcoding → full audio,
  trivially downloadable (this is what yt-dlp grabs). `policy: MONETIZE`, `snipped:false`.
- **Big-label official distributions:** strip `progressive`, serve **encrypted HLS** (`cbc/ctr-encrypted-hls`)
  + a plain `hls mp3_1_0` fallback that still yields full audio.
- **SoundCloud Go+ subscriber-only:** the *only* tier that is preview-only (`snipped:true`) when
  anonymous — and the unreleased long tail is never in this bucket.
Resolve a transcoding: `GET {transcoding.url}?client_id=<id>` → `{ "url": "<cdn stream>" }`.
This belongs to the **acquisition pipeline (yt-dlp → OCI)**, a separate plan; the built `Resolve`
method is the natural feed.

## 6. Costs & risks

- **`client_id` rotation** — breaks when SoundCloud cycles the public key; auto-resolve + yt-dlp
  fallback absorb it. A few times/month, not per request.
- **Rate limits** — `api-v2` throttles; reuse the per-provider circuit breaker (automatic via the
  fan-out) and keep page counts bounded. **Do not run concurrent evals** — they trip the limit and
  taint results.
- **ToS** — grey area, accepted for self-hosted use.
- **Public-only** — private/unlisted never surface.
- **Ranking gate** — any new *search* kind (album/artist) competes in ranking; gate with the eval.

## 7. Current implementation state

- `services/go-api/internal/discovery/adapters/providers/soundcloud_apiv2.go` — direct api-v2 client:
  `client_id` resolver, paginated track search, `Resolve(permalink)`, rich `extras`, 500px artwork.
- `soundcloud.go` — the original yt-dlp adapter, retained as **fallback** when `client_id`
  resolution is down.
- Wired in `internal/app/app.go` `buildDiscoveryProviders` (api-v2 primary, yt-dlp fallback).
- Verified: live "Ken Carson Olympics" head-to-head (old: 0 SC results; new: leak surfaces) and
  full v2 eval **99.1% top-3** (≥ 99.0% baseline, 17 vs 18 failures, no new regressions).
