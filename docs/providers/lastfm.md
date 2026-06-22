# Last.fm maximization

> Status: ✅ audited — live-probed 2026-06-22 (status codes + real field dumps against
> `ws.audioscrobbler.com/2.0`).
> Built: thin name **search** (track/album/artist), **charts** (`chart.gettop*` → vocabulary),
> **artist content** (`artist.gettoptracks` / `gettopalbums`), and — as of 2026-06-22 — the
> **detail-open `*.getInfo` enrichment** (cap 3: listen popularity + weighted tags + bio + MBID
> bridge) **with similar artists folded in** (cap 4, from `artist.getInfo.similar`). `LastFmEnricher`
> → `LastFmEnrichmentService` → `GET /discovery/enrichment/lastfm` → mobile `useLastFmEnrichment` →
> `LastFmEnrichmentSection`. Display-only, off the ranking path, no eval gate. Still unbuilt: the
> dedicated similar-**tracks** rail (`track.getSimilar` — needs a `/feature-spec` surface decision)
> and tag-as-discovery (cap 5).

## 1. Why this provider matters

Last.fm is the **listening-behavior + relatedness** provider. It fills the two metadata axes that
both maximized providers (MusicBrainz, Discogs) explicitly lack:

- **Listen-based popularity.** `listeners` + `playcount` per artist/track/album, from real scrobble
  data (verified: Kendrick Lamar 5.17M listeners / 1.05B plays; *HUMBLE.* 2.30M / 31.8M; *DAMN.*
  3.19M / 207M). This is a genuine *listening* signal — distinct from MB's sparse community `rating`
  and Discogs's `have`/`want` *collector* demand. The MB doc names "no listen-based popularity
  signal" as an open gap; this is it. (Deezer's `nb_fan`/`rank` stays the ranking primary — see §6.)
- **The relatedness graph.** `artist.getSimilar` and `track.getSimilar` return ranked neighbours with
  a `match` score (verified: Kendrick → Baby Keem 1.0, Jay Rock 0.89, JID 0.86, …; *HUMBLE.* → DNA.
  1.0, N95 0.59, The Box 0.37, …). Nothing else we have gives a *mainstream-wide* "similar to X"
  graph — SoundCloud's related-tracks only covers the underground long tail. This is the spine for a
  "Similar artists / Similar tracks" surface and a seed for the consensus→ML direction.
- **Weighted folksonomy tags.** `artist.gettoptags` / `*.getInfo.toptags` carry community tags with
  counts (Kendrick: Hip-Hop 100, rap 70, west coast 19, … jazz rap 5). Partial overlap with MB's
  `tags[]`, but Last.fm is the *source* and carries the weighting MB drops.
- **Tag-as-discovery.** `tag.gettopartists` turns any tag into a ranked artist feed (verified: "jazz
  rap" → McKinley Dixon, Mach-Hommy, Kendrick, Pink Siifu, …) — a genre-agnostic discovery primitive
  that fits the multi-user-diversity goal.
- **A second MBID bridge.** `*.getInfo` returns the entity's `mbid` (verified: Radiohead, *DAMN.*) —
  a cheap way to attach an MBID to a result, which is the key the MB/CAA/Fanart artwork chain depends
  on. Complementary to MB's own `url-rels` bridge.

It complements, does not duplicate: MB = identity + curated genres + artwork; Discogs = credits +
styles + label/catalog; **Last.fm = listening popularity + relatedness + weighted tags + bio/wiki.**

**Not an artwork source** (verified, §4) — artist images are a shared placeholder and album images
cap at 300px. Artwork stays an MB-keyed concern.

## 2. Access model

- **Tier 2 — official public API.** Base: `https://ws.audioscrobbler.com/2.0/`. Documented, stable,
  free. `&format=json` (default is XML). No internal/undocumented tier worth chasing.
- **Auth — API key** (query param `api_key=`). Already configured: `cfg.LastFMAPIKey`
  (`LASTFM_API_KEY`), gated by `cfg.HasLastFM()`. `LASTFM_SHARED_SECRET` is also set but only needed
  for *authenticated write* methods (scrobbling, love/unlove) — **none of the read methods here use
  it**. No rotation/self-heal needed (static key, not a bootstrapped public token).
- **Rate limit — ~5 req/sec per key** `[INFERRED]` from Last.fm's published terms; **not exposed in
  response headers** (verified: no `X-RateLimit-*` / `Retry-After` on a 200 — only `HTTP/1.1 200 OK`).
  More generous than MB's hard 1/sec, comparable headroom to Discogs's 60/min, but per-result detail
  lookups still demand caching (§6).
- **ToS / reach.** Sanctioned, documented, non-commercial use; attribution expected. No grey area
  (unlike SoundCloud). A descriptive `User-Agent` is polite; the key is the real gate.

## 3. Entity model

Maps cleanly to our `ResultKind` — no impedance mismatch:

| Last.fm entity | our `ResultKind` | notes |
|---|---|---|
| `artist` | `artist` | carries `mbid`, `stats.{listeners,playcount}`, `tags[]`, `similar[]`, `bio` |
| `track` | `track` | carries `mbid`, `listeners`, `playcount`, `duration` (ms), `album`, `toptags[]`, `wiki` |
| `album` | `album` | carries `mbid`, `listeners`, `playcount`, `tracks[]` (w/ duration, rank), `tags[]`, `wiki` |
| `tag` | (genre/discovery vocabulary) | `reach`/`total`, `wiki`, and `tag.gettop*` feeds — not a `ResultKind` |

Key nuance: **`match` on the similar feeds is a 0–1 relatedness weight**, and `playcount`/`listeners`
arrive as **strings** on some methods (search, getInfo) and **numbers** on others (toptracks,
getsimilar) — the existing adapter already normalizes via `parseListeners`.

## 4. Endpoint catalog (verified 2026-06-22)

| Endpoint | Returns | HTTP | Maps to |
|---|---|---|---|
| `?method=track.search&track=` | track matches (name, artist, url, `listeners`, image) | 200 | `SearchProvider` (track) ✅ built |
| `?method=album.search&album=` | album matches (name, artist, url, image) | 200 | `SearchProvider` (album) ✅ built |
| `?method=artist.search&artist=` | artist matches (name, url, `listeners`, image) | 200 | `SearchProvider` (artist) ✅ built |
| `?method=artist.gettoptracks&artist=` | an artist's top tracks (`playcount`, `listeners`) | 200 | `ArtistContentProvider` ✅ built |
| `?method=artist.gettopalbums&artist=` | an artist's top albums (`playcount`) | 200 | `ArtistContentProvider` ✅ built |
| `?method=chart.gettopartists` / `chart.gettoptracks` | global charts (name, `listeners`) | 200 | `ChartProvider` (vocabulary) ✅ built |
| `?method=artist.getinfo&artist=` | `mbid`, `stats.{listeners,playcount}`, `ontour`, `tags[]`, `similar[]` (top 5), `bio.{summary,content}` | 200 | **new: artist enrichment + MBID bridge** |
| `?method=track.getinfo&artist=&track=` | `mbid`, `listeners`, `playcount`, `duration`, `album`, `toptags[]`, `wiki` | 200 | **new: track enrichment** |
| `?method=album.getinfo&artist=&album=` | `mbid`, `listeners`, `playcount`, `tracks[]` (name/duration/rank), `tags[]`, `wiki` | 200 | **new: album enrichment + tracklist** |
| `?method=artist.getsimilar&artist=&limit=` | ranked similar artists with `match` (0–1) | 200 | **new: similar-artists surface** |
| `?method=track.getsimilar&artist=&track=&limit=` | ranked similar tracks with `match` + `playcount` | 200 | **new: similar-tracks rail (mainstream)** |
| `?method=artist.gettoptags&artist=` | weighted folksonomy tags (`name`, `count` 0–100) | 200 | **new: weighted tags** |
| `?method=tag.gettopartists&tag=&limit=` | ranked artists for a tag | 200 | **new: tag→discovery feed** |
| `?method=tag.getinfo&tag=` | `reach`, `total`, `wiki` | 200 | **new: tag/genre vocabulary** |

## 5. Capabilities to maximize

### 1. Name search (track/album/artist) — ✅ BUILT
`Search` over `track.search` / `album.search` / `artist.search`, mapping only the thin fields (name,
artist, url, `extralarge` image, and `listeners` into `extras` on track/artist). Code: `lastfm.go`.
**Gap:** search does **not** return tags, similar, playcount, mbid, or bio — those need a `getInfo`
lookup (capability 3). Album search carries no popularity at all.

### 2. Charts + artist content — ✅ BUILT
`FetchCharts` (`chart.gettop{artists,tracks}` → `VocabularyEntry` with `Popularity`) feeds the
vocabulary/chart surface; `GetArtistTopTracks` / `GetArtistAlbums` implement `ArtistContentProvider`
(wired as `"lastfm"` in the artist-content dispatch). Off the ranking path. Pulls the thin
projection; the per-track `playcount`/`listeners` are mapped but the richer detail is ignored here.

### 3. Detail-open enrichment via `*.getInfo` — ✅ BUILT (2026-06-22, the headline)
The highest-value addition, mirroring `musicbrainz-enrichment` / the Discogs enricher. One lookup per
opened entity yields:
- **`listeners` + `playcount`** — the listen-based popularity signal (verified accurate, §1).
- **Weighted `tags[]` / `toptags[]`** — genre/mood folksonomy with counts.
- **`bio` / `wiki`** — prose biography (artist) and release/track blurb (album/track). Overlaps
  Discogs `profile`; Last.fm's is CC-licensed and often more current.
- **`mbid`** — attach to the result to feed the MB enrichment + CAA/Fanart artwork chain (§1).
- **`duration` + album `tracks[]`** (with per-track duration/rank) — a tracklist source.
- Risk: **off the ranking path** (display/enrichment only) unless popularity feeds rank — if it does,
  it must clear the `discoveryeval --top-k 3` gate like every ranking change (§6).
- **Built:** `LastFmEnrichment` domain value object; `LastFmEnricher` port (`Lookup(kind, artist,
  title)` — kind-dispatched `*.getInfo`, `autocorrect=1`, no separate resolve step);
  `lastfm_enrichment.go` adapter (getInfo mappers + HTML/`Read more` bio cleaner, tolerant of the
  empty-collection-as-`""` quirk); `RedisLastFmEnrichmentCache` (read-through, 30d positive / 24h
  negative); `LastFmEnrichmentService` (best-effort, name-keyed); `GET /discovery/enrichment/lastfm`;
  mobile `useLastFmEnrichment` + `LastFmEnrichmentSection`. The MBID is surfaced in the payload (the
  MB-bridge seed) but artwork stays MB-keyed (§5.6) — not threaded into the artwork chain.

### 4. Relatedness graph (`artist.getSimilar` / `track.getSimilar`) — 🟡 PARTIAL (similar artists BUILT)
Ranked neighbours with a 0–1 `match` weight (verified, §1). Two surfaces:
- **Similar artists** → ✅ **BUILT** — folded into the artist enrichment payload (cap 3), since
  `artist.getInfo` already returns `similar[]` (top 5). Rendered as a "Similar artists" line on artist
  detail; no separate endpoint needed.
- **Similar tracks** → ⬜ **NOT BUILT** — a "Similar on Last.fm" rail, the *mainstream* counterpart to
  SoundCloud's underground `RelatedTracksProvider`. Could be a second implementation of that same
  port, or merged into a unified related surface — the surface (rail vs merged, SC-only vs unified) is
  a `/feature-spec` decision, deliberately deferred (like related-tracks was).
This is the seed for the consensus→ML relatedness direction. Display/enrichment only; any feed into
*order* is eval-gated.

### 5. Tag-as-discovery (`tag.gettopartists` / `tag.getinfo`) — ⬜ NOT BUILT
Turns any tag into a ranked artist feed (verified: "jazz rap" → McKinley Dixon, Mach-Hommy, …) plus
tag `reach`/`total`/`wiki`. A genre-agnostic discovery primitive aligned with the multi-user
diversity goal. Lowest priority of the three unbuilt caps — defer until a tag/genre-browse feature
exists to consume it.

### 6. Artwork — ❌ NOT A SOURCE (verified, do not wire)
`*.getInfo.image[]` tops out at **300px** (`extralarge`/`mega` both resolve to a `300x300` URL —
verified on *DAMN.*), below Discogs's 600 and far below CAA's 1200. Worse, **artist images are a
shared placeholder** — Radiohead returns the well-known default star hash
(`…/2a96cbd8b46e442fc41c2b86b821562f.png`), i.e. Last.fm no longer serves real artist art. The
existing search mapper reads `extralarge` but should be treated as last-resort/ignored. **Keep
artwork MB-keyed.**

## 6. Costs & risks

- **Per-result lookups vs ~5/sec.** Maximizing (caps 3–5) means a `getInfo`/`getSimilar` lookup per
  opened entity. Same mitigations as MB/Discogs, in order: (a) **only enrich what the user opens**
  (detail-open, never the fan-out); (b) **cache by `(method, artist[, track/album])` with a long
  TTL** (Redis — Last.fm data drifts slowly; mirror the `RedisEnrichmentCache` /
  `RedisDiscogsEnrichmentCache` pattern, positive ~30d / negative ~24h); (c) the rate ceiling is not
  header-exposed, so the circuit breaker must key off `429`/`5xx` bodies, not headers. **No blocking
  Last.fm call on the hot search path.**
- **Popularity into ranking → eval gate.** `playcount`/`listeners` is a real listen-based signal, but
  **Deezer (`nb_fan`/`rank`) stays the popularity primary** (per the pipeline design decisions). Any
  Last.fm popularity feed into *order* must clear `discoveryeval --top-k 3`. Pure display does not.
- **Tags ≠ curated genres.** Dedup Last.fm tags against MB's curated `genres[]`; don't stack the
  decade/mood noise ("2017") MB deliberately drops.
- **Not an artwork upgrade** (§5.6) — 300px ceiling + placeholder artist images.
- **String/number type drift.** `listeners`/`playcount` are strings on search/getInfo, numbers on
  toptracks/getsimilar — normalize via the existing `parseListeners` everywhere.
- **ToS:** sanctioned non-commercial use; key required, attribution expected. No grey area.

## 7. Current implementation state

Thin projection (caps 1–2), built and on `main`:

- `services/go-api/internal/discovery/adapters/providers/lastfm.go` — `LastFmAdapter`: `Search`
  (track/album/artist via `*.search`), `GetArtistTopTracks` / `GetArtistAlbums`
  (`ArtistContentProvider`), `FetchCharts` (`ChartProvider`), `lastfmExternalID` (url → id),
  `parseListeners` (string→int64 normalizer). Maps the thin fields + `listeners` into `extras`.
- Wired in `internal/app/app.go` (`buildDiscoveryProviders` as search provider; `artistProviders`
  map for discography; `FetchCharts` in the chart set) and `search_wiring.go` (artist-content
  dispatch). Config-gated by `cfg.HasLastFM()` (`LASTFM_API_KEY`).
- Covered by httptest fixtures in `lastfm_test.go` (search track/artist, missing-listeners,
  HTTP-error paths).

Detail-open enrichment (cap 3 + cap-4 similar artists), built 2026-06-22, mirroring
`musicbrainz-enrichment` / the Discogs enricher:

- `domain/lastfm_enrichment.go` — the `LastFmEnrichment` value object (+ `Empty`/`IsZero`).
- `ports/ports.go` — `LastFmEnricher` (single `Lookup(kind, artist, title)`) + `LastFmEnrichmentCache`.
- `adapters/providers/lastfm_enrichment.go` — the kind-dispatched `*.getInfo` lookups (`autocorrect=1`),
  tag/similar parsers tolerant of the empty-collection-as-`""` quirk, and `cleanLastFmBio` (strips the
  trailing `Read more on Last.fm` anchor + remaining HTML, unescapes entities). Shapes from the live probe.
- `adapters/cache/lastfm_enrichment_cache.go` — `RedisLastFmEnrichmentCache`, read-through, name-keyed
  (positive 30d, negative 24h); nil client = no-op.
- `service/lastfm_enrichment.go` — `LastFmEnrichmentService` (translates the wire `(kind, title,
  subtitle)` to Last.fm's `(artist, entityTitle)`; cache → lookup → cache; best-effort, always nil error).
- `adapters/handler/discovery_handler.go` — `GET /discovery/enrichment/lastfm?kind=&title=&subtitle=`
  (`WithLastFmEnrichment` setter) + DTO.
- `internal/app/app.go` — wired, config-gated by `cfg.HasLastFM()`; nil degrades to an empty DTO.
- Mobile: `shared/api-client/discovery.ts` (`getLastFmEnrichment` + `LastFmEnrichmentResponse`),
  `features/detail/hooks/useLastFmEnrichment.ts`, `features/detail/ui/LastFmEnrichmentSection.tsx`
  (compact popularity + tag pills + similar-artists line for artists + bio/blurb for tracks/albums —
  the artist bio is left to the Discogs section to avoid duplication), wired into `DetailScreen` below
  the hero next to the MB `EnrichmentSection`.
- Covered by httptest fixtures (adapter), fakes (service), and RNTL (hook + section). Backend 1023
  tests green; mobile 276 tests green.

## 8. Next steps

Caps 1–3 and the similar-artists half of cap 4 are built. Remaining:

1. **Similar-tracks rail (cap 4, second half).** `track.getSimilar` as a "Similar on Last.fm" rail —
   the *mainstream* counterpart to SoundCloud's underground `RelatedTracksProvider`. The surface (rail
   vs merged related set; SC-only vs unified) is a `/feature-spec` decision, deliberately deferred.
   Backend is then a thin `getSimilar` mapper, likely a second `RelatedTracksProvider` implementation.
2. **Tag-as-discovery (cap 5).** Lowest priority — defer until a tag/genre-browse feature exists to
   consume `tag.gettopartists`.
3. **MBID → artwork bridge (optional).** The enrichment surfaces the entity's `mbid`; a future
   increment could thread it into the MB/CAA/Fanart artwork chain to widen HD-art coverage for
   Last.fm-sourced cards. Not wired today — artwork stays MB-keyed (§5.6).

**Optional, eval-gated:** feeding Last.fm `playcount`/`listeners` into rank as a secondary
popularity signal behind Deezer (must clear `discoveryeval --top-k 3`).

**Not verifiable in this dev environment:** the real-world detail-open enrichment lift and any
ranking impact need the running pipeline + eval set + a device. All §4 endpoints were probed live
this session with the configured `LASTFM_API_KEY`; the adapter logic is covered by fixtures captured
from those probes.
