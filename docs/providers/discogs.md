# Discogs maximization

> Status: ✅ audited — live-probed 2026-06-22 (status codes + real field dumps against
> `api.discogs.com` and `i.discogs.com`).
> **Built: artwork fallback + artist identity consensus (caps 1–2).** The detail-open enrichment
> (credits, styles, label/catalog, community rating, artist bio and links, caps 3–7) is not built.
> The app shows no Discogs metadata today.

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
| `GET /artists/{id}` | `realname`, `profile`, `namevariations[]`, `aliases[]`, `members`/`groups`, `urls[]` (wikipedia/genius/whosampled/imdb/socials) | 200 | artist enrichment (not built); artwork + identity use it |
| `GET /artists/{id}/releases?sort=year` | paginated discography (mixed `master`/`release`, `role` incl. "Appearance" for features) | 200 | discography for identity consensus ✅ built |
| `GET /labels/{id}` | `name`, `profile`, `sublabels[]`, `parent_label`, `urls` | 200 | (label surface) |
| `GET https://i.discogs.com/...` (`images[].uri`) | 600×600 cover (`uri`) / 150px (`uri150`) | 200 | artwork fallback ✅ built (last, ≤600px) |

## 5. Capabilities to maximize

### 1. Artist-image artwork fallback — ✅ BUILT
`DiscogsAdapter.Resolve` implements `ports.ArtworkResolver` for `artist` only. `buildArtworkChain`
puts it with the key-gated resolvers, after CAA and Spotify. `ResolveByIdentity` skips the search when
the artist already carries a Discogs id. Searches artist → fetches `/artists/{id}` → returns the
`primary` image. Capped at 600px, so it only earns its place for artists the HD
ID-based sources miss. Code: `discogs.go`. **No further work** — this is the right scope for an
≤600px source.

### 2. Artist identity consensus — ✅ BUILT
`ResolveDiscogsArtist` (name + candidate album titles → best `DiscogsArtistInfo` via album-overlap
disambiguation) and `FetchArtistReleases` feed the consensus/identity engine off the ranking path.
Pulls only the **thin** projection (id, name, genre, country) — the rich credits/styles surface is
ignored here.

### 3–7. Detail-open enrichment: not built
The API carries more than the app uses. These are open opportunities, each detail-open and off the
ranking path:

- **Credits / personnel (3).** `master.tracklist[].extraartists[]` and release-level `extraartists[]`
  carry producer / written-by / mixed-by / mastered-by / featured, each with a Discogs artist id.
  Lookup path: structured `artist=+release_title=&type=master` → `master_id` → `/masters/{id}` +
  `/releases/{main_release}`. No other provider gives us this.
- **Styles (4).** `master.styles[]` / `release.styles[]`, the sub-genre layer MB lacks.
- **Label / catalog / formats / companies (5).** `release.labels[]` (+catno), `formats[]`,
  `country`, `released`, `companies[]`.
- **Community signal (6).** `release.community {have, want, rating}`. Display only; Deezer stays the
  popularity primary.
- **Rich artist metadata (7).** `/artists/{id}` `profile` (BBCode), `realname`, `aliases[]`,
  `namevariations[]`, `groups[]` / `members[]`, `urls[]`.

Anything here that feeds *order* must clear `discoveryeval --top-k 3`.

## 6. Costs & risks

- **No ISRC / no MBID — the matching risk.** Discogs identity is its own integer ids. Album credits
  require resolving a `master_id` via fuzzy `artist=+release_title=` search, which can pick the
  **wrong master or edition** (deluxe vs standard, reissue vs original). Mitigate: seed with the
  MB-bridge `external_ids.discogs` artist id to constrain candidates; prefer the `master` (abstract
  album) over a specific `release` for credits; verify artist-id match before trusting the master.
- **Per-result lookups vs 60/min.** Generous vs MB, but maximizing means a lookup per opened entity.
  Same mitigations as MB enrichment, in order: (a) **only enrich what the user opens** (detail-open,
  never the fan-out); (b) **cache by `master_id`/`artist_id` with a long TTL** (Redis — Discogs data
  is near-static). **No blocking Discogs call on the
  hot search path.**
- **Styles/community into ranking → eval gate.** Anything touching *order* must clear
  `discoveryeval --top-k 3`. Pure display enrichment does not.
- **Not an artwork upgrade.** 600px ceiling < CAA 1200px — keep artwork MB-keyed; Discogs stays
  metadata + the ≤600px artist fallback already wired.
- **ToS:** sanctioned; token + descriptive `User-Agent` required (403 without UA), attribution
  expected, non-commercial use fine.

## 7. Current implementation state

- `services/go-api/internal/discovery/adapters/providers/discogs/discogs.go`: `DiscogsAdapter`:
  `Resolve` / `ResolveByIdentity` (`ArtworkResolver`, artist images), `ResolveDiscogsArtist`
  (identity consensus via album-overlap), `FetchArtistReleases` (thin discography),
  `searchArtists` / `fetchArtistDetail` / `fetchArtistReleases`. A local limiter allows ~1 req/sec
  (well under the 60/min ceiling); `doGet` sets the `Discogs token=` auth + `User-Agent` and handles
  `429`.
- Wired in `internal/app/build_consensus_providers.go`: in `buildArtworkChain` (via
  `keyedArtworkResolvers`) and as a consensus provider (`discogsConsensusFetcher`). Both are
  config-gated by `cfg.HasDiscogs()` (`DISCOGS_TOKEN`).

## 8. Next steps

1. **Detail-open enrichment (caps 3–7).** Pick a surface first. Credits are the highest-value piece.
2. **Tighter artist matching.** Seed disambiguation with the MB-bridge `external_ids.discogs` artist
   id to cut wrong-entity picks.
