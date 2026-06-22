# iTunes / Apple Music maximization

> Status: ✅ audited — live-probed 2026-06-22 (status codes + real field dumps against
> `itunes.apple.com/search`, `itunes.apple.com/lookup`, and `is1-ssl.mzstatic.com` artwork).
> Built today: name **search** (track/album/artist), the **artwork fallback** (was 600px,
> **now 1500px** — cap 2), album **identity consensus** (`LookupAlbum`), 30s **preview URLs**
> mapped into track `extras`, and — as of 2026-06-22 — **discography / tracklist content via
> `/lookup`** (cap 5: `GetAlbumTracks` / `GetArtistTopTracks` / `GetArtistAlbums`), wired into the
> artist-detail albums union as a second mainstream source of truth alongside Deezer. The one *new
> metadata* axis is the **artwork ceiling raise** (cap 2); cap 5 is **corroboration for the
> multi-provider consensus**, not a new axis; the deeper Apple data stays gated behind a paid
> Apple Music developer token.

## 1. Why this provider matters

iTunes/Apple Music is the **keyless, sanctioned high-res-artwork source** and a reliable
mainstream-catalog search/identity provider. Against the already-maximized set (MusicBrainz =
identity + curated genres + CAA/Fanart artwork; Discogs = credits + styles; Deezer = lyrics + bpm
+ popularity primary; Last.fm = listen popularity + relatedness), its **one genuinely
non-duplicative contribution is artwork resolution**:

- **It raises the artwork ceiling — keyless.** Apple's artwork URLs are resolution-templated:
  the `…/100x100bb.jpg` thumbnail can be rewritten to any size, and Apple serves a **real
  3000×3000** master (verified §5.2). That is **above Cover Art Archive's 1200px** top tier — and
  unlike CAA (which needs an MBID), Apple art resolves from a plain keyless name search. So for
  the long tail no MBID covers, Apple is the **highest-resolution fallback we have** (above
  Deezer's 1000px and Discogs's 600px).
- **A clean, keyless mainstream search + identity surface.** `itunes.apple.com/search` needs **no
  key, no token, no User-Agent** (verified: bare `GET` → 200), mapping cleanly to track/album/
  artist. Already feeds the search fan-out and the album-contamination consensus check
  (`LookupAlbum`).

It **complements, does not duplicate**: the richer metadata axes are owned elsewhere, and Apple's
deeper data (editorial notes, lyrics, mood/activity, curated playlists) lives behind the **Apple
Music API**, which requires a **paid Apple Developer membership** + a MusicKit-signed JWT — out of
scope for the keyless tier (§6). What the keyless iTunes Search API adds beyond artwork is mostly
already covered: genre/explicit/release-date/duration (MB/Discogs/Deezer), 30s previews (Deezer
already carries `preview`), discography/tracklist via `lookup` (Deezer/MB/SC/Last.fm content ports).

**Not a popularity, lyrics, credits, or relatedness source** — it carries none of those keylessly.
Deezer stays the popularity primary; lyrics stay Deezer; credits stay Discogs.

## 2. Access model

- **Tier 2 — official public API.** Base: `https://itunes.apple.com`. The sanctioned iTunes Search
  API. There is **no internal/undocumented tier worth chasing keylessly** — the richer surface is
  the *Apple Music API* (`api.music.apple.com`), which is gated, not undocumented (§6).
- **Auth — none.** No key, no token, **no `User-Agent` required** (verified: bare `GET` → 200).
  Lowest-friction provider we have alongside Deezer's public API. No bootstrap, no rotation, no
  self-heal — nothing to cache.
- **Rate limit — ~20 requests/min per IP** `[INFERRED]` from Apple's published affiliate/Search-API
  terms; **not header-exposed** (verified: no `X-RateLimit-*` / `Retry-After` on a 200 — only
  `Server: daiquiri/5` and `x-apple-*` correlation keys). Tighter than Deezer/Last.fm/Discogs, so
  per-result lookups still demand caching (§6). The circuit breaker keys off status, not headers.
- **ToS / reach.** Fully sanctioned, documented use; public catalog only. **No grey area** (unlike
  SoundCloud's `client_id` or Deezer's `pipe` lyrics path). Artwork on `is1-ssl.mzstatic.com` is a
  public CDN.

## 3. Entity model

Maps cleanly to our `ResultKind` — no impedance mismatch:

| iTunes entity (`wrapperType`/`kind`) | our `ResultKind` | notes |
|---|---|---|
| `track` / `song` | `track` | carries `previewUrl` (30s AAC), `trackTimeMillis`, `trackNumber`/`discNumber`, `trackExplicitness`, `contentAdvisoryRating`, `collectionId`, `isStreamable` |
| `collection` / `Album` | `album` | carries `copyright` (℗ line), `trackCount`, `collectionExplicitness`, `releaseDate`, `collectionId` |
| `artist` / `Artist` (`musicArtist`) | `artist` | thin: `artistName`, `artistLinkUrl`, `primaryGenreName`, **`amgArtistId`** (the AllMusic id), `primaryGenreId` — **no artist image** |

Key nuances:
- **Identity is Apple's own integer ids** (`artistId`/`collectionId`/`trackId`) — **no ISRC, no
  MBID**. The one cross-provider bridge id present is **`amgArtistId`** (AllMusic), which MB's
  `url-rels` already bridges — so it adds little to the existing identity graph.
- **The `musicArtist` entity carries no artwork** (verified: an artist search returns only
  `artistName`/ids/genre, no `artworkUrl*`). Apple art is **album/track-keyed only** — so the
  artwork fallback is meaningful for `album`/`track`, not `artist`.
- **Censored vs uncensored** names both present (`trackName` + `trackCensoredName`); we use the
  uncensored `trackName`/`collectionName`.

## 4. Endpoint catalog (verified 2026-06-22)

| Endpoint | Returns | HTTP | Maps to |
|---|---|---|---|
| `GET /search?term=&entity=song&limit=` | tracks: `trackName`, `artistName`, `collectionName`, `previewUrl`, `artworkUrl100`, `trackTimeMillis`, `trackNumber`/`discNumber`, `trackExplicitness`, `contentAdvisoryRating`, `primaryGenreName`, `isStreamable` | 200 | `SearchProvider` (track) ✅ built |
| `GET /search?term=&entity=album&limit=` | albums: `collectionName`, `artistName`, `artworkUrl100`, `copyright`, `trackCount`, `collectionExplicitness`, `releaseDate`, `primaryGenreName` | 200 | `SearchProvider` (album) ✅ built + `LookupAlbum` consensus ✅ built |
| `GET /search?term=&entity=musicArtist&limit=` | artists: `artistName`, `artistLinkUrl`, `amgArtistId`, `primaryGenreName` — **no artwork** | 200 | `SearchProvider` (artist) ✅ built |
| `GET /lookup?id={artistId}&entity=album&limit=` | first result is the artist, rest are that artist's albums (`collectionName`, `releaseDate`, `copyright`, …) | 200 | discography (duplicative — **not wired**) |
| `GET /lookup?id={collectionId}&entity=song` | first result is the collection, rest are the album's tracks (`trackName`, `trackNumber`) | 200 | album tracklist (duplicative — **not wired**) |
| `GET https://is1-ssl.mzstatic.com/image/thumb/…/{N}x{N}bb.jpg` | resolution-templated cover: **1200→383KB, 3000→2.4MB real; 100000 → HTTP 400** | 200 | **artwork fallback ✅ built (now 1500px)** |

Artwork resolution probe (verified actual pixels via JPEG SOF header):
`1200x1200bb`→1200², `3000x3000bb`→3000², `5000x5000bb`→5000² (upscaled past the master),
`100000x100000bb`→**400 JSON error**. The `bb` (white-background-fill) suffix and its absence
return identical bytes. The master is **≥3000px** for modern catalog.

## 5. Capabilities to maximize

### 1. Name search (track/album/artist) — ✅ BUILT
`Search` over `/search?entity={song|album|musicArtist}&limit=15`, mapping the thin fields + 30s
`previewUrl` (track), `trackTimeMillis`→`duration`, and `primaryGenreName`→`genre` into `extras`.
Code: `itunes.go`. **Gap:** search does not return `copyright`, `trackNumber`, `contentAdvisory`,
or the high-res artwork master — those that matter are either covered elsewhere or fetched by the
artwork resolver (cap 2). Competes in ranking like any search kind → an eval gate guards any
*new* search behavior (none added here; the artwork change is off the ranking path).

### 2. **Artwork fallback — high-res** — ✅ BUILT (2026-06-22, the headline; was 600px → now 1500px)
The single non-duplicative metadata win. `Resolve` implements `ports.ArtworkResolver` via a
1-result search; the returned `artworkUrl100` is rewritten to the requested resolution. **The
ceiling was artificial**: the adapter upscaled to 600px when Apple serves a **real 3000×3000**
master (verified §4) — above Cover Art Archive's 1200px. Bumped the **detail-open hero** request
to **1500px** (`iTunesHeroArtworkSize`): comfortably past CAA's 1200, but a fraction of the ~2.4MB
a 3000px hero costs on mobile data. The **search-list thumbnail** stays a modest 600px
(`iTunesListArtworkSize`) — a card doesn't need the master, and bloating the search payload helps
nothing. Wired **after** the MBID-keyed sources in `buildArtworkChain` (CAA → Fanart → Genius →
TheAudioDB → Deezer → **iTunes** → YouTube → SoundCloud), so it only fires when the exact ID-keyed
sources miss — the long tail where a name-matched 1500px Apple cover beats the prior 600px.
Album/track-keyed only (the `musicArtist` entity carries no artwork, §3). **Display-only, off the
ranking path, no eval gate** — mirrors Deezer cap 2 exactly. The constant can be raised toward 3000
for a maximal hero if mobile-data cost is acceptable (one-line change).

### 3. Album identity consensus (`LookupAlbum`) — ✅ BUILT
`LookupAlbum` searches `/search?entity=album&limit=5` and returns an `AlbumVerdict`
(confirmed/contamination/unknown) + the iTunes `artistId`, used by the resolver for cross-album
artist-identity consistency (name + genre-overlap check, type-suffix stripped). Off the ranking
path; feeds the consensus engine alongside MB/Discogs. Thin projection only.

### 4. 30s preview URLs — ✅ MAPPED (no dedicated surface)
Track search carries `previewUrl` (a 30s AAC `m4a`), already mapped into track `extras` as
`preview_url`. A preview-play affordance is a **mobile feature, not metadata** — and Deezer tracks
already carry a `preview` URL, so this duplicates an existing axis. No new work needed for
metadata maximization; a unified preview-play surface (if ever built) is a `/feature-spec`.

### 5. Discography / tracklist via `/lookup` — ✅ BUILT (2026-06-22 — a second mainstream source of truth)
`/lookup?id={artistId}&entity=album` returns an artist's discography; `/lookup?id={collectionId}
&entity=song` returns an album's tracklist; `/lookup?id={artistId}&entity=song` returns the artist's
tracks (all verified live: the **first** result is the parent wrapper — `wrapperType: "artist"` or
`"collection"` — followed by the children). These implement `AlbumContentProvider` +
`ArtistContentProvider`. **Why built (not deferred):** discovery is moving to multi-provider
*consensus*, and a second mainstream discography/tracklist source strengthens the union and the
MB-validated album set — it is corroboration, not redundancy. **Built:** `GetAlbumTracks` /
`GetArtistTopTracks` / `GetArtistAlbums` on the adapter, fed by a shared `lookupContent(id, entity)`
that filters to the requested *child* `wrapperType` (`song`→`track`, `album`→`collection`) — which
drops the parent uniformly, including the album→song case where the parent is itself a `collection`.
Registered as `"itunes"` in the `albumProviders` / `artistProviders` dispatch maps (`app.go`).
**Prerequisite fixed:** an iTunes album/artist result now carries its own `collectionId`/`artistId`
in its `SourceRef` (previously every kind carried `trackId`, leaving album/artist results with an
unusable `"0"` — see §3 / `itunesSourceRef`); the change is **merge-neutral** (a SourceRef id only
affects merge via the xref-gated cross-provider bridge, and MB url-relations never carry an Apple
id, so a real Apple id can never bridge-match). Mobile: the artist-detail albums union
(`useArtistContent`) now fans out to iTunes alongside Deezer + SoundCloud, `artistName` passed through
so the same MB consensus validation applies; the album-tracklist path already keys off the result's
first source generically, so it picks up iTunes with no further change. **Caveat:** iTunes `/lookup`
songs are **catalog-ordered (recent-first), not popularity-ranked** — `GetArtistTopTracks` returns
"the artist's tracks", trimmed by the caller's limit (Deezer's `/artist/{id}/top` stays the real
"top"). Off the ranking path, no eval gate.

### 6. Apple Music API (editorial / lyrics / mood / curated) — ❌ GATED (paid token)
The richer surface — editorial notes ("why this album matters"), **time-synced lyrics**,
mood/activity classification, curated playlists, full catalog relationships — lives on
`api.music.apple.com`, gated behind a **paid Apple Developer membership** ($99/yr) and a
**MusicKit-signed developer JWT** (ES256, private-key-signed, ~6-month expiry). This is a real
paywall, not a reverse-engineering target. Most of what it adds we already have keylessly from
others: **lyrics → Deezer** (synced + plain, the headline of that provider), genres → MB, popularity
→ Deezer/Last.fm. Revisit only if an Apple-exclusive axis (editorial notes, Apple's mood taxonomy)
becomes worth the membership + key-management cost. **Out of scope for the keyless tier.**

## 6. Costs & risks

- **The ~20 req/min ceiling is the real cost.** Tighter than every other audited provider and
  **not header-exposed**. Maximizing means per-result artwork lookups → cache aggressively. Same
  mitigations, in order: (a) **only resolve artwork on detail-open**, never the fan-out; (b)
  **cache by `(kind, title, subtitle)` with a long TTL** (artwork URLs are static); (c) circuit
  breaker keys off status, not headers. **No blocking iTunes call on the hot search path.**
- **No ISRC / no MBID — name-match risk.** Artwork resolves by name search, so a wrong-entity
  match is possible (deluxe vs standard, a cover vs the original). This is exactly why iTunes sits
  **after** the MBID-keyed sources in the chain — it only fires when the exact sources miss, and a
  name-matched cover is strictly better than no cover. Same posture as the Deezer/Discogs fallbacks.
- **Artist artwork is absent.** The `musicArtist` entity carries no image — iTunes art is album/
  track-keyed only. Don't expect it to cover artist heroes (MB/Fanart own that).
- **Hero-artwork byte cost.** A 3000px master is ~2.4MB; we request 1500px (~600KB) as the
  sweet spot above CAA's 1200. Raising the constant trades mobile bandwidth for sharpness.
- **Apple Music API is a paywall, not a hack** (§5.6). The deep axes cost a paid membership + a
  signed JWT to unlock; most duplicate keyless axes we already have.
- **Ranking gate.** Anything from here that touches *order* must clear `discoveryeval --top-k 3`.
  The cap-2 artwork change is pure display — no gate.

## 7. Current implementation state

Built and on `main`, **unconditionally wired** (no config gate — the public API needs no key,
like Deezer):

- `services/go-api/internal/discovery/adapters/providers/itunes.go` — `ITunesAdapter`: `Search`
  (track/album/artist via `/search`), `Resolve` (`ArtworkResolver`, **now 1500px hero** via
  `iTunesHeroArtworkSize`; search-list thumbnails at `iTunesListArtworkSize` = 600), `LookupAlbum`
  (album-contamination consensus → `AlbumVerdict` + `artistId`), `upscaleArtwork` (the
  `100x100`→`NxN` URL rewrite), `stripITunesTypeSuffix`. Maps `previewUrl`/`duration`/`genre` into
  `extras`.
- Wired in `internal/app/search_wiring.go`: `buildArtworkChain` appends iTunes **after** the
  MBID-keyed sources (CAA → Fanart → Genius → TheAudioDB → Deezer → **iTunes** → YouTube →
  SoundCloud); also constructed as a search provider + album validator in `app.go`. No config gate.
- Covered by httptest fixtures in `itunes_test.go` (search track/artist, `LookupAlbum` table,
  HTTP-error path, and the new `Resolve` high-res assertion). Discovery suite green (build + vet
  clean, full `internal/discovery/...` tests pass).

## 8. Next steps

Cap 2 (the artwork ceiling raise) is the maximization — it's the one non-duplicative metadata axis
iTunes adds, and it's built. Remaining items are **optional, not coverage gaps**:

1. **Cache the artwork fallback** (cap 2 follow-on). The ~20 req/min ceiling makes a read-through
   cache (name-keyed, long positive TTL) worthwhile if iTunes-fallback resolves become frequent —
   mirror `RedisEnrichmentCache`. Not blocking; the chain already runs iTunes last.
2. **Raise the hero constant toward 3000px** if a sharper full-screen hero is wanted and the
   mobile-data cost (~2.4MB/open) is acceptable. One-line change to `iTunesHeroArtworkSize`.
3. **`/lookup` content ports** (cap 5) — ✅ **BUILT** (2026-06-22). A second mainstream discography/
   tracklist source for the consensus union. Optional follow-on: surface iTunes top-tracks on the
   artist screen (today only its albums join the union; its `/lookup` songs are catalog-ordered, not
   popularity-ranked, so they were not wired into the single "top tracks" rail).
4. **Apple Music API** (cap 6) — gated behind a paid membership + signed JWT; revisit only for an
   Apple-exclusive axis (editorial notes, mood taxonomy). Most of its data we already have keylessly.

**Not verifiable in this dev environment:** the real-world artwork-coverage lift on live traffic
(needs the running pipeline + a device). All §4 endpoints + the artwork-resolution ceiling were
probed live this session; the adapter logic is covered by httptest fixtures.
