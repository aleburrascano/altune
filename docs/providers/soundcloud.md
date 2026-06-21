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

### 4. Artist discography — ✅ BUILT
`GetArtistTopTracks` (`/users/{id}/toptracks`) and `GetArtistAlbums` (`/users/{id}/albums`)
implement `ports.ArtistContentProvider`, wired as `"soundcloud"` in the artist-content dispatch map
(`app.go`). `externalID` is the numeric user id a SoundCloud-sourced artist result already carries
in its `SourceRef`, so no separate id-resolution is needed. Reuses the track/album mappers.
Read-only enrichment, off the ranking path — no eval gate.

### 5. Related tracks (recommendations) — ✅ BUILT (Unit C — `docs/specs/related-tracks/`, spec+plan; 2026-06-21)
`/tracks/{id}/related` → `GetRelatedTracks` on the api-v2 adapter (reuses `mapSoundCloudAPITrack`),
a `RelatedTracksProvider` port, `GetRelatedTracksService`, and the
`GET /discovery/tracks/{provider}/{externalId}/related` route. Mobile: `useRelatedTracks` (SC-gated)
feeds a "Related on SoundCloud" rail in `TrackDetailBody`. SoundCloud-only, off the ranking path,
no eval gate. The original capability-5 surface notes are kept below for reference.
`/tracks/{id}/related` returns a recommendation set — live probe surfaced underground collabs
(e.g. "Lil Tecca & Ken Carson – Fell In Love") that pure search misses. The *data* is one endpoint
(`externalID` = the track's numeric id, already in its `SourceRef`); the *feature* is what's
undefined, which is why this is **not** another adapter bolt-on:
- **Where does it surface?** A "Related" rail on the detail screen? Inline under a result?
- **When is it called?** On detail open, on demand, prefetched?
- **Whose recs?** SoundCloud-only, or merged with other providers' related signals?
Decide these in a `/feature-spec`, then the adapter method is trivial (one endpoint, reuse
`mapSoundCloudAPITrack`).

### 6. Audio acquisition — 🟨 CODE-COMPLETE, UNVERIFIED END-TO-END (Unit D — `docs/specs/acquire-soundcloud/`, 2026-06-21)
> **Not "done".** The code + unit tests are written, but every test mocks `Download` and `Store` — the
> two steps that actually touch SoundCloud and OCI. Done requires a real run: save a SoundCloud-sourced
> track on a live backend (yt-dlp + SoundCloud reachable + OCI bucket) and confirm it downloads the
> *correct, full* MP3, lands in OCI, goes `ready`, and plays back. That run has not happened.
**Scope correction (2026-06-21):** the acquisition *pipeline* was already built (the `acquire-track`
spec: search → select → download → tag → store → mark-ready, the yt-dlp searcher, OCI object store,
the background scheduler, the `AcquisitionStatus` state machine). It was **YouTube-only**. So Unit D
is not "build the pipeline" — it's wiring SoundCloud in. Shipped (`acquire-soundcloud`): the acquisition
searcher now fans each query out to **both** `ytsearch5:` and `scsearch5:` and merges candidates, so the
existing Topic-first selection picks a SoundCloud upload when YouTube lacks the track (the underground
long tail). yt-dlp downloads SC URLs natively, so download/tag/store are unchanged. **Deferred** (own spec):
*direct-permalink acquisition* — carrying the discovered SC URL through save→acquire to skip the metadata
re-search and grab the exact track. The transcoding details below inform that deferred path.

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
This belongs to the **acquisition pipeline (yt-dlp → OCI)**, a separate plan — it touches storage,
OCI, and the existing `AcquisitionStatus` lifecycle in the catalog domain, so it is a full feature
loop, not an adapter method. The built `ResolvePermalink` (link → track) is the natural feed.

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

Capabilities 1–4 are **built and committed** on branch `refactor/discovery-pipeline-clarity`
(unpushed, solo branch):

| Capability | Commit |
|---|---|
| 1. Track search + `ResolvePermalink` | `b74e0ee` |
| 2. Album + artist search | `0b4d7ff` (eval-gated 99.1% top-3) |
| 3. Artwork resolver | `4762c91` |
| 4. Artist discography | `6751b84` |

- `services/go-api/internal/discovery/adapters/providers/soundcloud_apiv2.go` — the direct api-v2
  client: `client_id` resolver (self-healing), paginated track search, album/artist/track search,
  `ArtworkResolver`, `ArtistContentProvider`, `ResolvePermalink`, 500px artwork.
- `soundcloud.go` — the original yt-dlp adapter, retained as **fallback** when `client_id`
  resolution is down.
- Wired in `internal/app/app.go` (`buildDiscoveryProviders` as search provider; `artistProviders`
  map for discography) and `search_wiring.go` (`buildArtworkChain`, last).
- Verified: live "Ken Carson Olympics" head-to-head (old: 0 SC results; new: leak surfaces) and
  full v2 eval **99.1% top-3** (≥ 99.0% baseline, 17 vs 18 failures, no new regressions).

## 8. Next steps (updated 2026-06-21)

Adapter-level maximization is **done** (1–4). Units C and D have now landed too:

1. **Unit C — related tracks (capability 5).** ✅ **BUILT** — `docs/specs/related-tracks/`
   (spec + plan). Backend: `RelatedTracksProvider` port, `GetRelatedTracks` on the api-v2 adapter,
   `GetRelatedTracksService`, `GET /discovery/tracks/{provider}/{externalId}/related`. Mobile:
   `useRelatedTracks` (SC-gated) → "Related on SoundCloud" rail in `TrackDetailBody`. SoundCloud-only,
   off the ranking path (no eval gate). 483 discovery tests green; 7 new mobile tests green.
2. **Unit D — audio acquisition (capability 6).** 🟨 **CODE-COMPLETE, UNVERIFIED END-TO-END** —
   `docs/specs/acquire-soundcloud/` (spec + plan). **Not "done":** the logic + wiring are written and
   unit-tested, but the tests mock `Download` and `Store`, so nothing has proven real audio is acquired
   correctly. **Done bar:** a live save of a SoundCloud-sourced track downloads the correct full MP3 into
   OCI, goes `ready`, and plays back — not yet run (needs a running backend + yt-dlp + OCI + a device).
   **Scope was smaller than this doc implied:** the acquisition pipeline already existed (`acquire-track`)
   and was YouTube-only, so the work was wiring SoundCloud in, not building the pipeline. Two increments
   written:
   - **Dual-engine search** — the searcher queries `scsearch5:` alongside `ytsearch5:` and merges, so
     Topic-first selection can pick a SoundCloud upload when YouTube lacks the track.
   - **Direct-source acquisition (the correctness fix)** — when a saved result carries the exact
     SoundCloud URL the user discovered, acquisition downloads *that exact track* (skipping the lossy
     re-search that can grab a wrong reupload), falling back to search on failure. The URL rides
     `CreateTrackRequest.source_url` → `Schedule(…, sourceURL)` → `Execute(…, sourceURL)`; pass-through,
     no migration. The insight: the pipeline almost always downloads *something*, so the real problem is
     *wrong* audio, not *no* audio. SoundCloud is the only discovery provider that is also yt-dlp-
     downloadable, so the exact path is the SoundCloud path.

   **Still deferred:** *persisting* the source URL on the Track (needs a schema migration, human-
   reviewed) — until then, retries / stream-reacquire fall back to search. §5.6 has the transcoding
   details a future "persist + best-source-selection" pass would consume.

**Not verifiable in this dev environment** (same limits as the rest of the pipeline): a live
`scsearch5:` hit + SC-URL download + OCI store needs yt-dlp, SoundCloud network access, and OCI
credentials. Unit-tested via seams; the live path reuses the already-working YouTube download code.
