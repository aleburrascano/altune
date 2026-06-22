# Deezer maximization

> Status: ✅ audited — live-probed 2026-06-22 (status codes + real field dumps against
> `api.deezer.com`, `www.deezer.com/ajax/gw-light.php`, `auth.deezer.com`, and `pipe.deezer.com`).
> Built today: name **search** (track/album/artist), the **cover/artist-image artwork fallback**
> (now 1000px `_xl` — cap 2), **charts** (`chart/0/*` → vocabulary), **album/artist content** (album
> tracks, artist top/albums), **ISRC fetch** (`/track/{id}` → `isrc`), and — as of 2026-06-22 — the
> **detail-open enrichment** (cap 7 track audio fields `bpm`/`gain`/explicit; cap 8 album liner data
> `label`/`genres`/`upc`/`record_type`) via `DeezerEnricher` → `DeezerEnrichmentService` →
> `GET /discovery/enrichment/deezer` → mobile `useDeezerEnrichment` → `DeezerEnrichmentSection`.
> Display-only, off the ranking path, no eval gate. Still unbuilt: **lyrics (synced + plain)** —
> deliberately a separate feature (cap 6).

## 1. Why this provider matters

Deezer is already our **popularity primary** — `nb_fan`/`rank` drive ranking (per the pipeline design
decisions), and nothing displaces it. As a *metadata* source it has one axis no other audited provider
touches (lyrics — still to build), plus the detail surface now surfaced on detail-open (caps 7–8):

- **Lyrics — the uncovered axis.** MusicBrainz, Discogs, and Last.fm carry **no lyrics**. Deezer's
  internal GraphQL (`pipe.deezer.com`) returns **full plain text + time-synced lines**
  (`lrcTimestamp`/`milliseconds`/`duration`) + **songwriter `writers`** + publishing `copyright`
  (verified: *Hello* → 44 synced lines, writers "Adele Laurie Blue Adkins, Gregory Allen Kurstin").
  Time-synced lyrics are a marquee detail-screen / playback feature; this is the only wired provider
  that can supply them.
- **Per-track audio fields.** The public `/track/{id}` carries **`bpm`** (verified populated: *Lose
  Yourself* 171.6, *Without Me* 112.3 — sparse/0 on some, e.g. instrumental-leaning tracks) and
  **`gain`** (ReplayGain, verified always present). Audio-adjacent metadata that also seeds the
  deferred ML-audio direction — nothing else we have carries it.
- **Contributor credits with roles.** `/track/{id}.contributors[]` and `/album/{id}.contributors[]`
  carry each contributor + **`role`** ("Main", "Featured", …) and their Deezer artist id. A lighter
  credit surface than Discogs's per-track `extraartists[]`, but free, fast, and from a provider we
  already call.
- **Album liner data.** `/album/{id}` carries **`upc`**, **`label`**, **`genres[]`**, `record_type`,
  `fans`, and `nb_tracks` (verified: *Discovery* → UPC 724384960650, label "Daft Life Ltd./ADA
  France", genre Electro). Complements Discogs's deeper catalog data.

It complements, does not duplicate: MB = identity + curated genres + artwork; Discogs = deep credits +
styles + label/catalog; Last.fm = listen popularity + relatedness + tags; **Deezer = lyrics + audio
fields (bpm/gain) + the popularity primary + a light credit/liner surface.**

**Not the artwork primary** — but a real fallback: `cover_xl`/`picture_xl` resolve to **1000×1000**
(verified), above Discogs's 600 and Last.fm's 300, below Cover Art Archive's 1200. Album art stays
MB/CAA-keyed; the existing Deezer artwork fallback (500px `_big`) could be bumped to 1000px `_xl` for
the long tail the HD ID-based sources miss.

## 2. Access model

Two tiers, both reachable, with sharply different ToS posture:

- **Tier 2 — official public API.** Base: `https://api.deezer.com`. **No auth, no key, no
  `User-Agent` required** (verified: bare `GET` → 200). This is what the search/artwork/charts/content
  adapter uses today. Documented, stable, sanctioned.
  - **Rate limit — ~50 requests / 5 seconds per IP** `[INFERRED]` from Deezer's published terms; **not
    probed via headers this session** (no `X-RateLimit-*` observed on a 200). Generous; per-result
    detail lookups still demand caching (§6).
- **Tier 1 — internal APIs (for lyrics).** Two distinct internal surfaces, both reverse-engineered:
  1. **`www.deezer.com/ajax/gw-light.php`** — the legacy web backend. Bootstrap: `GET
     ?method=deezer.getUserData&input=3&api_version=1.0&api_token=` (with a cookie jar) → returns
     `results.checkForm` (the CSRF `api_token`) and sets the `sid` session cookie (verified). But
     `method=song.getLyrics` returns **`{"DATA_ERROR":"No lyrics id for <id> and country CA"}`** for an
     *anonymous* session (verified on two vocal tracks) — **lyrics here are gated behind a logged-in
     account (ARL)**. Not the path to use.
  2. **`pipe.deezer.com/api` (GraphQL) — the working anonymous lyrics path.** Bootstrap: `GET
     https://auth.deezer.com/login/anonymous?jo=p&rto=c&i=c` → returns an **anonymous JWT** (~539
     chars, verified works **standalone** — no SID cookie needed). Then `POST https://pipe.deezer.com/api`
     with `Authorization: Bearer <jwt>` and the `SynchronizedLyrics` query → full lyrics (verified 200
     + real synced lines). Bespoke bootstrap = the anonymous-JWT fetch; cache + self-heal on `401`.
- **ToS / reach.** The public API is fully sanctioned. The `pipe` GraphQL lyrics access is
  **reverse-engineered, against ToS** — same grey-area posture as SoundCloud's internal API, accepted
  for this project's self-hosted personal/family use, named explicitly (per the README ToS doctrine).
  Public-only reach; nothing private surfaces.

## 3. Entity model

Maps cleanly to our `ResultKind` — no impedance mismatch:

| Deezer entity | our `ResultKind` | notes |
|---|---|---|
| `track` | `track` | carries `isrc`, `bpm`, `gain`, `contributors[]` (+`role`), `explicit_lyrics`, `rank`, `preview`, `track_token` |
| `album` | `album` | carries `upc`, `label`, `genres{data[]}`, `record_type`, `fans`, `nb_tracks`, `contributors[]` |
| `artist` | `artist` | carries `nb_fan`, `nb_album`, pictures to `picture_xl` (1000px) |

Key nuances (already encoded in the adapter / pipeline notes): **album search returns `nb_fan=0`** (the
pipeline falls back to kind-local positional popularity); **`bpm` is `0` on a non-trivial fraction of
tracks** (treat as "unknown", not "0 BPM"); **`rank` is higher = more popular** (used directly, not
inverted).

## 4. Endpoint catalog (verified 2026-06-22)

| Endpoint | Returns | HTTP | Maps to |
|---|---|---|---|
| `GET /search/{track\|album\|artist}?q=&order=RANKING` | thin matches | 200 | `SearchProvider` ✅ built |
| `GET /track/{id}` | `isrc`, `bpm`, `gain`, `contributors[]` (+role), `explicit_lyrics`, `rank`, `release_date`, `preview`, `track_token`, `available_countries`, `md5_image` | 200 | ISRC ✅ built; **bpm/gain/credits new** |
| `GET /album/{id}` | `upc`, `label`, `genres{data[]}`, `record_type`, `fans`, `nb_tracks`, `contributors[]`, `cover_xl` (1000px) | 200 | **album liner enrichment (new)** |
| `GET /artist/{id}` | `nb_fan`, `nb_album`, `picture_xl` (1000px) | 200 | content ✅ built |
| `GET /album/{id}/tracks` · `/artist/{id}/top` · `/artist/{id}/albums` | content lists | 200 | `Album/ArtistContentProvider` ✅ built |
| `GET /chart/0/{tracks\|artists\|albums}` | global charts | 200 | `ChartProvider` ✅ built |
| `GET /ajax/gw-light.php?method=deezer.getUserData` | `results.checkForm` token + `sid` cookie | 200 | (lyrics bootstrap, legacy) |
| `GET /ajax/gw-light.php?method=song.getLyrics` (anon) | `DATA_ERROR: No lyrics id … country CA` | 200 | ❌ auth-gated — do not use |
| `GET auth.deezer.com/login/anonymous?jo=p&rto=c&i=c` | anonymous `jwt` (standalone) | 200 | **lyrics bootstrap (working)** |
| `POST pipe.deezer.com/api` (`SynchronizedLyrics`, Bearer jwt) | `lyrics{ text, synchronizedLines[], writers, copyright }` | 200 | **lyrics enrichment (new, headline)** |

`SynchronizedLyrics` query body (verified):
```graphql
query SynchronizedLyrics($trackId: String!) {
  track(trackId: $trackId) {
    id
    lyrics { id copyright text writers
             synchronizedLines { lrcTimestamp line milliseconds duration } }
  }
}
```
Lyrics availability is **per-track and region-dependent**: *Hello* → 44 synced lines; *Lose Yourself*
→ `LyricsNotFoundError` (a structured GraphQL error, not an auth failure — the standalone JWT is valid).

## 5. Capabilities to maximize

### 1. Name search (track/album/artist) — ✅ BUILT
`Search` / `SearchStructured` over `/search/{kind}` with `order=RANKING`, mapping the thin fields +
`isrc`, `rank`, `nb_fan`, `duration`, `preview_url` into `extras`. Code: `deezer.go`. **Gap:** search
does not return `bpm`, `gain`, contributors, `upc`, `label`, or lyrics — those need the detail lookups
(caps 6–8).

### 2. Cover / artist-image artwork fallback — ✅ BUILT (1000px `_xl`)
`Resolve` implements `ports.ArtworkResolver` via a 1-result search, skipping `IsDeezerPlaceholder`.
**Bumped to 1000px** (`cover_xl`/`picture_xl`, falling back to `_big` 500px when absent) — helps the
long tail the HD ID-based sources (CAA 1200 / Fanart) miss. Display-only, off the ranking path.

### 3. Charts — ✅ BUILT
`FetchCharts` over `chart/0/{tracks,artists,albums}` → `VocabularyEntry` with popularity. Feeds the
vocabulary surface. Off the ranking path.

### 4. Album / artist content — ✅ BUILT
`GetAlbumTracks`, `GetArtistTopTracks`, `GetArtistAlbums` implement the content ports. Pulls the thin
projection; the rich `/track/{id}` and `/album/{id}` fields are ignored here.

### 5. ISRC fetch — ✅ BUILT
`FetchTrackISRC` (`/track/{id}` → `isrc`) and `FetchFirstTrackID` feed the identity/consensus engine.
Already hits `/track/{id}` — but reads **only** `isrc`, discarding the bpm/gain/credits on the same
response (cap 7).

### 6. **Lyrics — synced + plain** (`pipe.deezer.com` GraphQL) — ⬜ NOT BUILT (the headline)
The single highest-value addition and the one axis no other provider gives us. Anonymous-JWT bootstrap
→ `SynchronizedLyrics` → `text` (plain), `synchronizedLines[]` (LRC-style timed lines), `writers`,
`copyright`. Surfaces as a lyrics view on track detail (and, with `synchronizedLines`, a karaoke/scrub
sync during playback once that surface exists). **Detail-open only, off the ranking path** — mirror the
`musicbrainz-enrichment` / Discogs / Last.fm enricher pattern exactly: a `DeezerLyrics` value object,
a `LyricsProvider` port, a read-through Redis cache (long positive TTL — lyrics are static; short
negative TTL — availability is region/catalog dependent), a `LyricsService`, and a
`GET /discovery/lyrics` (or `/discovery/enrichment/deezer/lyrics`) endpoint + mobile hook/section. The
anonymous JWT needs a cached bootstrap with `401` self-heal (the rotation tax, lighter than
SoundCloud's `client_id`). **ToS: reverse-engineered — name it explicitly** (§6).

### 7. **Rich track metadata** (bpm/gain/explicit) — ✅ BUILT (2026-06-22)
`Lookup(track)` fetches `/track/{id}` and maps `bpm` (rounded; 0 = unknown, rendered only when `> 0`),
`gain` (ReplayGain — carried in the payload but **not displayed**: a volume-normalization value, not
user-facing), and `explicit_lyrics`. `DeezerEnrichmentSection` shows "172 BPM · Explicit" on track
detail. **Contributors are deliberately excluded** — the detail screen's existing three-tier
featured-artists path already fetches `/track/{id}` contributors, so re-surfacing them here would
duplicate. Display-only; if `bpm` ever feeds *order*, that increment must clear `discoveryeval --top-k 3`.

### 8. **Album liner data** (label/genres/upc/record_type) — ✅ BUILT (2026-06-22)
`Lookup(album)` fetches `/album/{id}` and maps `label`, `genres{data[]}` (deduped/capped),
`record_type`, and `upc` (payload only — a barcode, not user-facing). `DeezerEnrichmentSection` shows
the label line + genre pills on album detail. A light album-enrichment surface — thinner than Discogs's
catalog/companies data, but free and fast from a provider already in the fan-out. Display-only, off the
ranking path.

## 6. Costs & risks

- **Two ToS tiers.** The public API is sanctioned; the **`pipe` GraphQL lyrics path is
  reverse-engineered and against ToS** — accepted for self-hosted personal/family use, named
  explicitly (README doctrine). Public-only reach.
- **Anonymous JWT rotation.** The `pipe` Bearer token expires; bootstrap (`auth.deezer.com/login/
  anonymous`) must be cached and re-fetched on `401`, singleflight-deduped — the same self-healing
  shape as SoundCloud's `client_id` resolver, but simpler (one GET, no JS-bundle scraping).
- **Per-result lookups vs ~50/5s.** Maximizing (caps 6–8) means a detail/lyrics lookup per opened
  entity. Same mitigations as the other enrichers, in order: (a) **only enrich what the user opens**
  (detail-open, never the fan-out); (b) **cache by id with a long TTL** (Redis — track/album data and
  lyrics are static); (c) the public rate ceiling is not header-exposed, so the circuit breaker keys
  off status, not headers. **No blocking lyrics/detail call on the hot search path.**
- **Lyrics availability is region/catalog-dependent.** `LyricsNotFoundError` is common and not an
  error condition — cache the negative, degrade to "no lyrics", never surface a failure.
- **`bpm` is sparse.** Populated for many tracks, `0` for others — render only when `> 0`; never
  display "0 BPM".
- **Not the artwork or popularity primary.** Deezer *is* the popularity primary already (`nb_fan`/
  `rank`); for *artwork* it's a 1000px fallback, below CAA. Don't re-task it.
- **Ranking gate.** Anything from here that touches *order* (bpm, contributor-derived signals) must
  clear `discoveryeval --top-k 3`. Pure display enrichment does not.

## 7. Current implementation state

Built and on `main` (search + artwork + charts + content + ISRC), thin projection only — and notably
**unconditionally wired** (no `cfg.HasDeezer()` gate, unlike MB/Discogs/Last.fm; the public API needs
no key):

- `services/go-api/internal/discovery/adapters/providers/deezer.go` — `DeezerAdapter`: `Search` /
  `SearchStructured` / `searchKind` (track/album/artist), `Resolve` (`ArtworkResolver`, 1000px `_xl`,
  placeholder-skipping), `GetAlbumTracks` / `GetArtistTopTracks` / `GetArtistAlbums`
  (`Album/ArtistContentProvider`), `FetchCharts` (`ChartProvider`), `FetchTrackISRC` /
  `FetchFirstTrackID` (identity/consensus). Maps `isrc`/`rank`/`nb_fan`/`duration`/`preview` into
  `extras`.
- Wired in `internal/app/app.go` three times: `buildDiscoveryProviders` (search, line ~405),
  `artistProviders`/content (line ~192), and the chart set (line ~453). No config gate.
- Covered by httptest fixtures in `deezer_test.go`.

Detail-open enrichment (caps 7–8), built 2026-06-22, mirroring the MB/Discogs/Last.fm enrichers:

- `domain/deezer_enrichment.go` — the `DeezerEnrichment` value object (+ `Empty`/`IsZero`).
- `ports/ports.go` — `DeezerEnricher` (`ResolveID` + `Lookup`) + `DeezerEnrichmentCache`.
- `adapters/providers/deezer_enrichment.go` — name-resolve via search, then `/track/{id}` (bpm round /
  gain / explicit) or `/album/{id}` (label / genres dedup / upc / record_type) lookup. Also bumped the
  `Resolve` artwork to 1000px `_xl` (cap 2). Response shapes from the live probe.
- `adapters/cache/deezer_enrichment_cache.go` — `RedisDeezerEnrichmentCache`, read-through, name-keyed
  (positive 30d, negative 24h); nil client = no-op.
- `service/deezer_enrichment.go` — `DeezerEnrichmentService` (cache → resolve → lookup; track/album
  only; best-effort, always nil error).
- `adapters/handler/discovery_handler.go` — `GET /discovery/enrichment/deezer?kind=&title=&subtitle=`
  (`WithDeezerEnrichment` setter) + DTO.
- `internal/app/app.go` — wired unconditionally (public API needs no key); nil cache degrades to uncached.
- Mobile: `shared/api-client/discovery.ts` (`getDeezerEnrichment` + `DeezerEnrichmentResponse`),
  `features/detail/hooks/useDeezerEnrichment.ts` (track/album-gated),
  `features/detail/ui/DeezerEnrichmentSection.tsx` (tempo/explicit for tracks, label/genres for albums),
  wired into `DetailScreen` below the track/album bodies.
- Covered by httptest fixtures (adapter), fakes (service), and RNTL (hook + section). Backend discovery
  tests green (576); mobile detail tests green (130).

## 8. Next steps

Caps 1–5, 7, 8, and the cap-2 artwork bump are built. The one remaining capability is lyrics,
deliberately scoped as its own feature:

1. **Lyrics enrichment (cap 6, the headline — separate feature).** `/feature-spec` the surface
   (track-detail lyrics view; timed-scroll during playback is a follow-on once the synced lines have a
   player to drive). Backend: anonymous-JWT bootstrap (cached, `401` self-heal), the `pipe`
   `SynchronizedLyrics` client, a `DeezerLyrics` value object, `LyricsProvider` port, `RedisLyricsCache`,
   `LyricsService`, `GET /discovery/lyrics`, mobile hook + section. Display-only, no eval gate. **Name
   the ToS posture in the spec** (the `pipe` GraphQL access is reverse-engineered, unlike the public-API
   enrichment shipped here).

**Optional, eval-gated:** feeding Deezer `bpm` or any new signal into *rank* (must clear
`discoveryeval --top-k 3`). Deezer `nb_fan`/`rank` already *is* the popularity primary — unchanged.

**Not verifiable in this dev environment:** the real-world lyrics coverage / synced-line accuracy and
the JWT-rotation cadence on live traffic (needs the running pipeline + a device). All §4 endpoints were
probed live this session (public API anonymously; `pipe` with a freshly-bootstrapped anonymous JWT);
field dumps above are real.
