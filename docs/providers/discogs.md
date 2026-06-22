# Discogs maximization

> Status: ✅ audited — live-probed 2026-06-22 (status codes + real field dumps against
> `api.discogs.com` and `i.discogs.com`).
> **Fully maximized (caps 1–7 built).** Artwork fallback + artist identity consensus + the
> detail-open **album** enrichment (credits/personnel, styles, label/catalog, formats/companies,
> community rating — caps 3–6) **and** the detail-open **artist** enrichment (bio, name history,
> group/member links, external links — cap 7). `DiscogsEnricher` (album + artist resolve/lookup) →
> `DiscogsEnrichmentService` / `DiscogsArtistEnrichmentService` →
> `GET /discovery/enrichment/discogs[/artist]` → mobile `useDiscogsEnrichment` /
> `useDiscogsArtistEnrichment` → `DiscogsEnrichmentSection` / `DiscogsArtistSection`. All
> display-only, off the ranking path, no eval gate.

## 1. Why this provider matters

Discogs is the **deepest structured-metadata source in music**, and it fills the exact gap
MusicBrainz leaves. MB gives us identity, curated `genres[]`, ratings, and the cross-provider
bridge — but MB has **no credits, no styles, no label/catalog, no per-track personnel**. Discogs is
the authority for precisely those:

- **Credits & personnel** — per-track *and* per-release: producer, written-by, mixed by, mastered
  by, featured/guest, executive-producer, recorded-at, A&R. Each credit carries the contributor's
  Discogs artist id (verified: DAMN. track 1 "Blood" → 9 credits incl. producers Anthony Tiffith /
  Bēkon, written-by Kendrick Duckworth). Nothing else we have carries this.
- **Styles** — finer sub-genres *below* genre (DAMN.: genres `Hip Hop, Funk / Soul`; styles
  `Conscious, Contemporary R&B, Jazzy Hip-Hop, Trap, Boom Bap`). MB has genres but not this layer.
- **Label, catalog number, formats, country, pressing** — per-edition release metadata MB models
  thinly. Plus **companies** (recorded at, mastered at, copyright/phonographic-copyright holders,
  distributed/manufactured by) — full liner-notes data.
- **Community signal** — `have` / `want` / `rating {average, count}` — a non-streaming
  demand+quality signal (DAMN. main release: have 2980, want 1946, rating 4.27 ×313).

It complements, not duplicates, MusicBrainz: MB = identity + genres + the bridge; Discogs = credits
+ styles + label/catalog + liner notes. **Not an artwork source** — its images cap at 600px
(verified), below Cover Art Archive's 1200px, so artwork stays an MB-keyed concern.

## 2. Access model

- **Tier 2 — official public API.** Base: `https://api.discogs.com`. Documented, stable. No
  internal/undocumented tier worth chasing (the site is rendered from this same API).
- **Auth — personal access token** (40-char), sent as `Authorization: Discogs token=<token>`.
  Already configured: `cfg.DiscogsToken` (`DISCOGS_TOKEN`), gated by `cfg.HasDiscogs()`. A
  descriptive `User-Agent` is **required** (else 403) — same posture as MusicBrainz. No rotation/
  self-heal needed (static personal token, not a bootstrapped public key like SoundCloud's
  `client_id`).
- **Rate limit — 60 req/min authenticated** (verified via `X-Discogs-RateLimit: 60`,
  `-Remaining`, `-Used` headers; `429` on exhaustion, already handled in `doGet`). Far more generous
  than MB's hard 1/sec — but per-result detail lookups still demand caching (§6).
- **ToS / reach.** Sanctioned, documented use; attribution + token required, non-commercial use
  fine. No grey area (unlike SoundCloud). Images on `i.discogs.com` are fetchable with only a
  `User-Agent` (verified `200 image/jpeg`), but the host is rate-limited.

## 3. Entity model

Discogs models *releases*, not recordings, and has **no ISRC and no MBID** — identity is its own
integer ids. The bridge *in* is MusicBrainz's `external_ids.discogs` (the **artist** id, from cap-4)
plus a structured `artist=+release_title=` search to resolve the master (§5).

| Discogs entity | our `ResultKind` | notes |
|---|---|---|
| `artist` | `artist` | carries realname, aliases, namevariations, members/groups, urls |
| `master` | `album` | the *abstract* album (all editions) — like MB's release-group. `master_id` is the stable album key; carries genres, styles, year, per-track tracklist+credits |
| `release` | (edition of an album) | a concrete pressing under a master; carries label/catno, formats, country, companies, identifiers (barcode), community stats, release-level credits |
| `label` | (no `ResultKind`) | label/catalog metadata, sublabels, parent_label |
| track | (inside `master.tracklist[]` / `release.tracklist[]`) | **not** a standalone searchable entity — credits live on the tracklist entry (`extraartists[]`), not a `/tracks/{id}` resource |

Key nuance: **credits hang off the `master`/`release`, not the artist** — so the MB bridge (which
gives the *artist* discogs id) is a disambiguation aid, not a direct key to album credits. Album
credits need a `master_id`, resolved by structured search (§5).

## 4. Endpoint catalog (verified 2026-06-22)

| Endpoint | Returns | HTTP | Maps to |
|---|---|---|---|
| `GET /database/search?q=&type=master\|release\|artist\|label` | matches with `genre[]`, `style[]`, `format[]`, `label[]`, `country`, `master_id` | 200 | search / album resolve |
| `GET /database/search?artist=&release_title=&type=master` | **deterministic album→`master_id`** (verified: Kendrick+DAMN → 1164779) | 200 | **enrichment lookup key** |
| `GET /database/search?barcode=` / `?catno=` | edition lookup by barcode / catalog no. | 200 | (edition resolve) |
| `GET /masters/{id}` | genres, styles, year, `main_release`, `tracklist[]` w/ per-track `extraartists[]` (credits), videos, images (≤600px), `num_for_sale`, `lowest_price` | 200 | **album enrichment (credits/styles)** |
| `GET /masters/{id}/versions` | editions (`title, format, country, released, label, catno`) | 200 | (edition list) |
| `GET /releases/{id}` | release-level `extraartists[]`, `labels[]` (+catno), `companies[]` (recorded/mastered/copyright), `formats[]`, `identifiers[]` (barcode), `genres/styles`, `community {have, want, rating}`, `notes` | 200 | **edition enrichment + community signal** |
| `GET /artists/{id}` | `realname`, `profile`, `namevariations[]`, `aliases[]`, `members`/`groups`, `urls[]` (wikipedia/genius/whosampled/imdb/socials) | 200 | artist enrichment ✅ partially built |
| `GET /artists/{id}/releases?sort=year` | paginated discography (mixed `master`/`release`, `role` incl. "Appearance" for features) | 200 | `ValidateArtistAlbums` / discography ✅ partially built |
| `GET /labels/{id}` | `name`, `profile`, `sublabels[]`, `parent_label`, `urls` | 200 | (label surface) |
| `GET https://i.discogs.com/...` (`images[].uri`) | 600×600 cover (`uri`) / 150px (`uri150`) | 200 | artwork fallback ✅ built (last, ≤600px) |

## 5. Capabilities to maximize

### 1. Artist-image artwork fallback — ✅ BUILT
`DiscogsAdapter.Resolve` implements `ports.ArtworkResolver` for `artist` only, wired **last** in
`buildArtworkChain` (after MB/CAA/Fanart/SoundCloud). Searches artist → fetches `/artists/{id}` →
returns the `primary` image. Capped at 600px, so it only earns its place for artists the HD
ID-based sources miss. Code: `discogs.go`. **No further work** — this is the right scope for an
≤600px source.

### 2. Artist identity consensus — ✅ BUILT
`ResolveDiscogsArtist` (name + candidate album titles → best `DiscogsArtistInfo` via album-overlap
disambiguation) and `FetchArtistReleases` feed the consensus/identity engine off the ranking path.
Pulls only the **thin** projection (id, name, genre, country) — the rich credits/styles surface is
ignored here.

### 3. Album & track **credits / personnel** — ✅ BUILT (the headline)
The single highest-value addition, and the one thing no other provider gives us. `master.tracklist[]
.extraartists[]` (per-track) + release-level `extraartists[]` carry producer / written-by / mixed-by
/ mastered-by / featured / executive-producer / recorded-at, each with a Discogs artist id. **Built:**
`LookupAlbum` prefers the release-level credit list (the curated album-wide set), falling back to the
deduped per-track set; `DiscogsEnrichmentSection` groups them by role on album detail. Lookup path:
structured `artist=+release_title=&type=master` → `master_id` → `/masters/{id}` +
`/releases/{main_release}`. Off the ranking path; detail-open, mirroring `musicbrainz-enrichment`.

### 4. **Styles** (sub-genre layer) — ✅ BUILT
`master.styles[]` / `release.styles[]` — finer than MB's curated genres (the layer MB lacks).
Surfaced as style pills on album detail. **Still display-only** — if ever fed into *order*
(genre/style signals), that increment must clear `discoveryeval --top-k 3`.

### 5. **Label / catalog / formats / companies** — ✅ BUILT
`release.labels[]` (+catno), `formats[]`, `country`, `released`, `companies[]` (recorded-at /
mastered-at / copyright / distributed-by). Assembled into the enrichment payload and rendered as the
label/catalog + format/country lines. (Barcode `identifiers[]` are available but not surfaced — not
user-facing.) Pure display enrichment, off ranking path.

### 6. **Community signal** (have/want/rating) — ✅ BUILT
`release.community {have, want, rating{average, count}}` — a non-streaming demand+quality signal,
rendered as a "N have · N want · ★ rating" line. Adds the **have/want demand** dimension MB lacks.
**Display/secondary only** — Deezer (`nb_fan`/`rank`) stays the popularity primary; not wired into
rank (would need the eval gate).

### 7. Rich artist metadata — ✅ BUILT
`/artists/{id}` carries the **biography** (`profile`, with BBCode stripped), **name history**
(`realname`, `aliases[]`, `namevariations[]`), **group/member relationships** (`groups[]` /
`members[]`), and **external links** (`urls[]` → wikipedia, genius, imdb, socials, official site).
**Built:** `ResolveArtistID` (name → artist id, exact-normalized-match preferred) + `LookupArtist`
(bio clean + link categorization by host) → `DiscogsArtistEnrichmentService` →
`GET /discovery/enrichment/discogs/artist?name=` → mobile `useDiscogsArtistEnrichment` →
`DiscogsArtistSection` (bio, a.k.a., member-of, tappable links). Display-only, off the ranking path.
The cross-provider ids in `urls[]` remain available to a future identity-graph widening increment.

## 6. Costs & risks

- **No ISRC / no MBID — the matching risk.** Discogs identity is its own integer ids. Album credits
  require resolving a `master_id` via fuzzy `artist=+release_title=` search, which can pick the
  **wrong master or edition** (deluxe vs standard, reissue vs original). Mitigate: seed with the
  MB-bridge `external_ids.discogs` artist id to constrain candidates; prefer the `master` (abstract
  album) over a specific `release` for credits; verify artist-id match before trusting the master.
- **Per-result lookups vs 60/min.** Generous vs MB, but maximizing means a lookup per opened entity.
  Same mitigations as MB enrichment, in order: (a) **only enrich what the user opens** (detail-open,
  never the fan-out); (b) **cache by `master_id`/`artist_id` with a long TTL** (Redis — Discogs data
  is near-static); (c) `discogs_cache.go` already exists to extend. **No blocking Discogs call on the
  hot search path.**
- **Styles/community into ranking → eval gate.** Anything touching *order* must clear
  `discoveryeval --top-k 3`. Pure display enrichment does not.
- **Not an artwork upgrade.** 600px ceiling < CAA 1200px — keep artwork MB-keyed; Discogs stays
  metadata + the ≤600px artist fallback already wired.
- **ToS:** sanctioned; token + descriptive `User-Agent` required (403 without UA), attribution
  expected, non-commercial use fine.

## 7. Current implementation state

Artwork + identity (the original thin projection):

- `services/go-api/internal/discovery/adapters/providers/discogs.go` — `DiscogsAdapter`:
  `Resolve` (`ArtworkResolver`, artist images), `ResolveDiscogsArtist` (identity consensus via
  album-overlap), `FetchArtistReleases` (thin discography), `searchArtists` / `fetchArtistDetail` /
  `fetchArtistReleases`. `rateLimit()` enforces ~1 req/sec locally (well under the 60/min ceiling);
  `doGet` sets the `Discogs token=` auth + `User-Agent` and handles `429`.

Detail-open album enrichment (caps 3–6, mirroring `musicbrainz-enrichment`):

- `domain/discogs_enrichment.go` — the `DiscogsEnrichment` **and** `DiscogsArtistEnrichment` value
  objects (+ `Empty`/`IsZero`).
- `ports/ports.go` — `DiscogsEnricher` (`ResolveMasterID`/`LookupAlbum` + `ResolveArtistID`/
  `LookupArtist`) + `DiscogsEnrichmentCache` + `DiscogsArtistEnrichmentCache`.
- `adapters/providers/discogs_enrichment.go` — the structured-search resolve + master/release lookup
  (credit dedup/cap, format/label/company/community mappers) **and** the artist resolve + lookup
  (BBCode profile cleaner, host→label link categorizer). Response shapes from the live probe.
- `adapters/cache/discogs_enrichment_cache.go` — `RedisDiscogsEnrichmentCache` (album) +
  `RedisDiscogsArtistEnrichmentCache` (artist), read-through, name-keyed (positive 30d, negative 24h);
  nil client = no-op.
- `service/discogs_enrichment.go` — `DiscogsEnrichmentService` + `DiscogsArtistEnrichmentService`
  (cache → resolve → lookup, all best-effort).
- `adapters/handler/discovery_handler.go` — `GET /discovery/enrichment/discogs?album=&artist=` and
  `/discovery/enrichment/discogs/artist?name=` (`WithDiscogsEnrichment` /
  `WithDiscogsArtistEnrichment`, non-breaking setters) + DTOs.
- `internal/app/app.go` — both wired, config-gated by `cfg.HasDiscogs()` (`DISCOGS_TOKEN`); nil
  degrades to an empty DTO.
- Mobile: `shared/api-client/discovery.ts` (`getDiscogsEnrichment` / `getDiscogsArtistEnrichment` +
  types), `features/detail/hooks/useDiscogsEnrichment.ts` (album-gated) +
  `useDiscogsArtistEnrichment.ts` (artist-gated), `features/detail/ui/DiscogsEnrichmentSection.tsx`
  (styles/credits/label/community) + `DiscogsArtistSection.tsx` (bio/aka/groups/links), wired into
  `DetailScreen` below the album and artist bodies.
- Covered by httptest fixtures from the live probe (adapter), fakes (service), and RNTL (hooks +
  sections). 550 discovery tests + 111 detail tests green.

## 8. Next steps

Caps 1–7 are built — the provider is maximized. Remaining are optional refinements, not coverage gaps:

1. **Tighter master/artist matching.** Both resolves are fuzzy (combined-title `contains` for albums,
   exact-name-else-top for artists; §5, §5.7 `AIDEV-DECISION`). Seed disambiguation with the MB-bridge
   `external_ids.discogs` artist id (thread it through from the enrichment surface) to cut wrong-entity
   picks.
2. **Styles-into-rank (cap. 4, optional).** Eval-gated increment if styles ever feed *order* rather
   than display.
3. **Community-into-rank (cap. 6, optional).** Same — display today; needs the `discoveryeval
   --top-k 3` gate before touching order.
4. **Cross-provider identity graph (cap. 7, optional).** The artist `urls[]` carry more provider ids
   than MB's bridge; a future increment could feed them into the merge/identity graph (eval-gated).

**Not verifiable in this dev environment:** the real-world `master_id` match accuracy on live traffic
(fuzzy `artist+title` resolution needs the running pipeline + an eval set + a device). All §4
endpoints were probed live this session with the configured `DISCOGS_TOKEN`; field dumps above are
real, and the adapter logic is covered by fixtures captured from those probes.
