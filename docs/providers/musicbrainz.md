# MusicBrainz maximization

> Status: ✅ audited — live-probed 2026-06-22 (status codes + real field dumps against
> `musicbrainz.org/ws/2` and `coverartarchive.org`).
> Built: name **search** + identity/consensus, the `inc=` enrichment **lookup** (genres/ratings/
> url-rels), the **Cover Art Archive** + **Fanart.tv** artwork tiers, the cross-provider **identity
> bridge** (cap 4) and **search-list MBID warm** (cap 5). Caps 4–5 ✅ (eval 2026-06-22: top-3 99.4%,
> no regression, ADR-0011 Accepted); cap 6 (Fanart.tv) ✅ live-verified 2026-06-21.

## 1. Why this provider matters

MusicBrainz is the **identity hub**, and that makes it the highest-leverage provider for *metadata
and artwork* — the two axes we actually care about now that acquisition is solved.

- **It raises the artwork ceiling.** Our two best artwork sources — Cover Art Archive (up to 1200px)
  and Fanart.tv (HD) — are **MBID-keyed**. A result with no MBID is permanently capped at
  name-search fallback artwork (iTunes 600px upscaled, Genius wrong-artist risk). MusicBrainz is how
  results *get* MBIDs, which is what unlocks the HD tier.
- **It is the cross-provider bridge.** A single artist lookup returns that artist's **Deezer,
  Spotify, Discogs, Last.fm, AllMusic, Genius, Wikidata** IDs (verified below). That is the spine for
  cross-linking one entity across every other provider — and the Wikidata/Deezer links feed the
  Deezer→MBID bridge the inventory still has open.
- **It is the metadata backbone.** Canonical names, ISRC, disambiguation, artist type, area, curated
  genres, folksonomy tags, relationships, authoritative release-groups.

Nothing else moves both axes at once: Discogs/Last.fm are metadata-only (Last.fm artwork is dead);
the artwork chain can't improve without the MBIDs only MusicBrainz supplies.

## 2. Access model

- **Tier 2 — official public API.** Base: `https://musicbrainz.org/ws/2`. Open, documented, free.
  No internal/undocumented tier to chase — this *is* the API the site and everyone else uses.
- **Auth — none.** No key. The only gate is a descriptive `User-Agent` (contact included) or you get
  `403`. Already set via `cfg.MusicBrainzUserAgent`.
- **Cover Art Archive** — base `https://coverartarchive.org`, no auth, no key, no published rate
  limit. Keyed by release / release-group MBID. Backed by the Internet Archive.
- **Rate limit — 1 req/sec** (hard). Enforced today by `rateLimit()` (mutex + sleep). This is the
  load-bearing cost of maximizing: see §6.
- **ToS / reach.** Fully sanctioned, non-commercial use; community CC0 data. No grey area (unlike
  SoundCloud). Bulk Postgres dumps exist for self-hosting if rate limits ever bite hard.

## 3. Entity model

Maps cleanly to our `ResultKind` — no impedance mismatch (contrast SoundCloud's "every uploader is a
user"):

| MusicBrainz entity | our `ResultKind` | notes |
|---|---|---|
| `artist` | `artist` | direct; carries type (Person/Group), area, life-span, IPIs/ISNIs, aliases |
| `recording` | `track` | direct; carries ISRCs |
| `release-group` | `album` | the *abstract* album (all editions); carries primary/secondary type, first-release-date |
| `release` | (edition of an album) | a concrete edition under a release-group; **this is the MBID Cover Art Archive art hangs off** |
| `genre` / `tag` | (enrichment) | curated genres vs folksonomy tags — distinct lists |
| `url` relation | (cross-provider bridge) | Deezer/Spotify/Discogs/Last.fm/Wikidata IDs |

Key nuance: **art is keyed by `release` MBID, not `release-group`** — but CAA accepts a release-group
MBID and returns the group's chosen front cover, resolving to a `/release/{release-mbid}/…` image URL
(verified §4). So album art only needs the release-group MBID we already map.

## 4. Endpoint catalog (verified 2026-06-22)

| Endpoint | Returns | HTTP | Maps to |
|---|---|---|---|
| `GET /ws/2/artist?query=&fmt=json` | artist matches (type, area, begin-area, life-span, aliases, IPIs/ISNIs) — **no genres** | 200 | `SearchProvider` (artist) ✅ built |
| `GET /ws/2/recording?query=&inc=isrcs&fmt=json` | recordings + ISRCs | 200 | `SearchProvider` (track) ✅ built |
| `GET /ws/2/release-group?query=&fmt=json` | release-groups (title, primary-type, first-release-date) | 200 | `SearchProvider` (album) ✅ built |
| `GET /ws/2/artist/{mbid}?inc=url-rels+genres+tags+ratings&fmt=json` | curated `genres[]`, `tags[]`, `rating`, `relations[]` (Deezer/Spotify/Discogs/Last.fm/Wikidata/AllMusic/Genius/official) | 200 | **new: enrichment + cross-provider bridge** |
| `GET /ws/2/release-group/{mbid}?inc=genres+ratings&fmt=json` | primary/secondary types, first-release-date, RG-level `genres[]`, `rating` | 200 | **new: album enrichment** |
| `GET /ws/2/release-group?artist={mbid}&type=album\|ep\|single&limit=100` | an artist's discography | 200 | `ValidateArtistAlbums` ✅ built (consensus) |
| `GET https://coverartarchive.org/release-group/{mbid}` | `images[]` — `front:true`, full `image`, `thumbnails {250,500,1200}` | 200 | **artwork (separate `coverartarchive.go`)** |

## 5. Capabilities to maximize

### 1. Name search (artist/track/album) — ✅ BUILT
`Search` / `SearchStructured` over `artist`/`recording`/`release-group`, `inc=isrcs` on tracks. Maps
only the thin fields (mbid, title, subtitle, isrc, disambiguation, type, area, tags). Code:
`musicbrainz.go`. **Gap:** search does **not** return curated genres or relationships — those need a
lookup (capability 3).

### 2. Identity resolution + album consensus — ✅ BUILT
`ResolveArtistIdentity` (name → MBID + disambiguation/birth-year/area/type), `ValidateArtistAlbums`,
`LookupAlbumArtist` (contamination check). Off the ranking path; feeds the consensus engine and the
identity resolver. This is the only place lookups happen today — and they fetch the *thin* projection.

### 3. Artist & album enrichment via `inc=` lookup — ✅ BUILT (`docs/specs/musicbrainz-enrichment/`, 2026-06-22)
The single highest-value addition. One lookup per resolved entity yields:
- **Curated `genres[]`** (with vote counts) — e.g. for Kendrick: *conscious hip hop, hip hop, jazz
  rap, west coast hip hop, trap*. Distinct from raw `tags[]` (which include decade/mood noise like
  "2010s"). This is real genre metadata for a genre-agnostic, multi-user library.
- **`rating`** (community rating + vote count) — a non-streaming popularity/quality signal.
- **Release-group**: `primary-type` (Album/EP/Single), `secondary-types` (Live/Compilation/
  Soundtrack/Remix — lets us *demote or label* non-canonical albums), `first-release-date` (the
  authoritative year).
- Risk: **off the ranking path** (display/enrichment only) unless we feed genres/rating into rank —
  if we do, it must clear the `discoveryeval --top-k 3` gate like every ranking change.

### 4. Cross-provider identity bridge (url-relations) — ✅ EXTRACTED + MERGE-USE DONE (ADR-0011 Accepted)
`inc=url-rels` on an artist returns its IDs on **Deezer (`deezer.com/artist/525046`), Spotify,
Discogs, Last.fm, AllMusic, Genius, Wikidata (`Q130798`), official homepage, IMDb, RateYourMusic,
BBC** (all verified). **Built (`musicbrainz-enrichment`):** the lookup parses `relations[]` into bare
ids (`external_ids{deezer,spotify,discogs,wikidata}` on `MBEnrichment`), returned on the detail
enrichment endpoint — available to the client now and the seed for the merge bridge. **Merge-use
built (ADR-0011):** an `IdentityBridge` port (the enrichment cache's `ExternalIDs` read side) feeds
those ids into `Merge` via a pre-merge `stampIdentities` pass, so a result merges by stated identity
(new `EntityResolutionBridge` tier) instead of name similarity — additive, cache-only, no hot-path MB
call. **Eval passed (2026-06-22):** top-3 99.4% (1782/1792), the highest recorded — no regression →
ADR-0011 Accepted. Still open: the full background-warm of the bridge (graduation path) and the
keep-apart override (un-merging same-name different entities) — separate increments.

### 5. Cover Art Archive — HD MBID-keyed album art — ✅ BUILT on detail-open (`musicbrainz-enrichment`, 2026-06-22)
`coverartarchive.org/release-group/{mbid}` → front cover at **250/500/1200px** (verified 200 + real
URLs). `coverartarchive.go` already runs **first** in `buildArtworkChain`. The unlock was never the
resolver — it's **MBID coverage**: the `EnrichmentService` resolves an MBID for the opened entity
(passed or strict name-resolved), then runs the artwork chain with it, so a Deezer/iTunes-sourced
album now returns its HD Cover Art Archive cover on detail-open instead of an upscaled name-search
thumbnail. The mobile hero upgrades to `artwork_url`. **Search-list lift — now largely delivered:**
(1) identity-merge (ADR-0011) means a merged Deezer+MB card carries the mbid, so the existing
search-path `enrich()` already upgrades it to HD CAA art on the list; (2) a cache-only `MBIDIndex`
(name→mbid memo, warmed by detail-opens) attaches an MBID to an *unmerged* non-MB result so its card
gets CAA/Fanart art too — zero MB calls on the search path. **Still deferred:** the background MBID-warm
worker for cold/never-opened entities (cap 5 graduation `b`).

### 6. Fanart.tv (HD artist images / album art) — ✅ BUILT + LIVE-VERIFIED (2026-06-21)
MBID-keyed, in `buildArtworkChain` behind `cfg.HasFanartTV()`. **Live-probed this session** with the
configured key: artist art is top-level on `/v3/music/{mbid}` (`artistthumb` → `artistbackground`);
album art is on the **dedicated** `/v3/music/albums/{mbid}` endpoint, nested under
`albums[mbid].albumcover`. Fixed a real bug — the adapter called the artist endpoint for albums and
read a non-existent top-level `albumcover` key (the prior album test fixture was fabricated; corrected
to the live shape). Fires only when the result carries an MBID (same dependency as CAA — now widened by
the cap-4 bridge + cap-5 `MBIDIndex`).

## 6. Costs & risks

- **The 1 req/sec wall is the real cost.** Maximizing means *per-result lookups* (cap. 3/4), which
  multiply request count against a hard 1/sec limit. Mitigations, in order: (a) **only enrich the
  results users see** — top-N after ranking, never the full fan-out; (b) **cache aggressively** —
  MB data is near-static, cache by MBID with a long TTL (Redis, like artwork); (c) the bulk Postgres
  dump is the escape hatch if we ever need volume. **Do not** add a blocking lookup to the hot search
  path — enrich lazily / on detail-open.
- **No popularity signal for ranking.** MB `rating` is sparse and community-driven, not listen-based —
  keep Deezer (`nb_fan`/`rank`) as the popularity primary. MB's value is metadata + identity, not rank.
- **Genres ≠ tags.** Use the curated `genres[]` list, not raw `tags[]` (decade/mood noise). Verified
  they are separate fields.
- **Ranking gate.** Anything from here that touches *order* (e.g. genre/rating signals) must clear
  `discoveryeval --top-k 3`. Pure display enrichment does not.
- **ToS:** none meaningful — sanctioned non-commercial use, CC0 data.

## 7. Current implementation state

Built and on `main` (search + identity/consensus), thin projection only:

- `services/go-api/internal/discovery/adapters/providers/musicbrainz.go` — `Search`,
  `SearchStructured`, `searchKind` (artist/recording/release-group, `inc=isrcs` on track),
  `ResolveArtistIdentity`, `ValidateArtistAlbums`, `LookupAlbumArtist`, `fetchReleaseGroups`. Maps
  `mbid` into `extras` on every result — the seed the artwork chain already depends on.
- `services/go-api/internal/discovery/adapters/providers/coverartarchive.go` — `ArtworkResolver`,
  wired **first** in `buildArtworkChain` (`search_wiring.go`). Already consumes release-group MBIDs.
- Wired in `internal/app/search_wiring.go`: `sharedMB` as `SearchProvider` + `WithAlbumValidator`;
  `BuildConsensusProviders` uses `ValidateArtistAlbums`. Config-gated by `cfg.HasMusicBrainz()`
  (`MUSICBRAINZ_USER_AGENT`).
- `rateLimit()` enforces 1 req/sec across all calls.

## 8. Next steps

Detail-open maximization (caps. 3 + 4-data + 5) **shipped** in `musicbrainz-enrichment` (2026-06-22):
the `inc=` enrichment lookup (`Lookup`), strict name resolution (`ResolveMBID`), the `MetadataEnricher`/
`EnrichmentCache` ports, the read-through `RedisEnrichmentCache`, the `EnrichmentService`, the
`GET /discovery/enrichment` endpoint, and the mobile `useEnrichment` hook + `EnrichmentSection` +
hero-artwork upgrade. Display-only, off the ranking path, no eval gate. Adapter covered by httptest
fixtures from the live probe.

Remaining:

1. **Identity-based merge (cap. 4 merge-use). — ✅ DONE (ADR-0011 Accepted, eval 2026-06-22).** The
   `external_ids` feed `Merge` via the `IdentityBridge` (cache-only, additive, `bridge` tier). Top-3
   eval 99.4% — no regression.
2. **Search-path enrichment (cap. 5 list-wide). — ✅ DONE (merged cards + `MBIDIndex` warm).**
   Merged Deezer+MB cards get HD CAA art via the existing `enrich()`; the cache-only `MBIDIndex`
   attaches an MBID to unmerged non-MB results. Covered by the same eval pass. Deferred: the
   background MBID-warm worker for cold entities (`b`).
3. **Fanart.tv (cap. 6). — ✅ DONE.** Live-probed with `FANARTTV_API_KEY`; fixed the album
   endpoint/nesting bug; tests now encode the real shape. Display-only, no eval gate.

**Not verifiable in this dev environment:** the real-world
MBID-coverage / artwork-upgrade lift on live traffic (needs the running pipeline + eval set + a device).
The adapter logic is covered by httptest fixtures captured from the live probe; §4 endpoints were all
probed live this session.
