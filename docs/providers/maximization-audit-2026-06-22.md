# Provider maximization audit — 2026-06-22

Cross-provider audit of every discovery provider in `services/go-api`, grounded in (a) two live prod coverage scans and (b) one deep research pass per provider (current adapter code + provider doc + web research into public/internal/scrape surface). Goal: find where each provider is **capped, broken, or under-mapped**, and rank concrete moves to squeeze more coverage and enrichment out of each.

**Status:** research/audit only. Survivors route to `/feature-spec` or `/feature-plan`; nothing here is implemented except the unique-reach diagnostic (below). Anything that touches search ordering must clear the `discoveryeval -mode eval -top-k 3` gate per `services/go-api/CLAUDE.md`.

**Confidence tags** carried from the research: **[C]** confirmed (read in code / official docs / live-probed this session) · **[I]** inferred (reasoned from API shape, not live-verified). Items marked [I] need a live probe before building.

---

## 1. What triggered this & the evidence

The question was "did the provider expansion (the `maximize youtube music` / `maximize itunes` commits + the SoundCloud/Discogs/Last.fm/Deezer enrichment adds) actually work?" Two diagnostics answer it, both run against the **prod** library (Supabase) from a local process:

- **`discoveryeval -mode eval -top-k 3`** — ranking quality. Baseline (last full run): 1792 entities → top-3 **98.9%** (1773), top-1 97.2%, 19 fails (all `track`).
- **`discoveryeval -mode signal-b`** — per-provider album-coverage imbalance across the 5 consensus providers (`lastfm`, `musicbrainz`, `itunes`, `ytmusic`; Deezer is the seed, not scanned).

### Coverage scan — with the new unique-reach measure

`ProviderGap.Unique` was added this session ([coverage_signal_b.go](../../services/go-api/internal/discovery/service/coverage_signal_b.go)): an entity whose provider-set has size 1 is reach only that provider brings. Prod run, 311 artists, 15,117 album-entity union:

| Provider | Covered | Gap % | **Unique reach** | Read |
|---|---|---|---|---|
| **Last.fm** | 12,821 | 15.2% | **9,937** (~66% of union) | The backbone — sole source for two-thirds of the album universe |
| **iTunes** | 2,877 | 75.9% | **1,704** | 3× YouTube's unique reach **while 403-crippled** (true number higher) |
| **YouTube Music** | 2,941 | 80.5% | **582** | The provider we set out to verify — adds the **least** unique reach |
| **MusicBrainz** | 0 | 100% | 0 | Dead contributor (bug §3.4) — not a real coverage stat |

**Caveat:** single-provider "unique" clusters are inflated by fuzzy-title match misses (an album one provider has under a title variant that didn't cluster). Treat unique numbers as directional, not exact.

### The reprioritization this produced

- **YouTube Music — the headline of the original "did it work" question — adds the least unique reach (582).** Most of what it returns, others already have. The heavy L-effort YT `browse` client drops down the priority list, justified by data.
- **iTunes, even losing half its calls to 403, contributes 3× more unique reach (1,704).** Fixing the 403 likely unlocks more coverage than building YT browse.
- **Last.fm holds 9,937 unique entities and is capped at 50 albums/artist (bug §3.3)** — the single most important coverage bug.

---

## 2. Cross-cutting themes

These patterns recur across providers and are higher-leverage than any single feature add:

### 2.1 The identity bridge is starved of keys it already pays to fetch
Multiple providers return cross-provider join keys in responses we already fetch, then discard them:
- **SoundCloud**: `publisher_metadata.isrc` rides on every `/search/tracks` result — unmapped. SC tracks float at `EntityResolutionTier.none` and never merge. Mapping it promotes SC from a coverage-only island to an identity participant. **[C]**
- **Discogs**: `release.identifiers[]` (Barcode/UPC) on the `/releases/{id}` we already fetch — never decoded. A UPC is the same key Deezer/iTunes use; it upgrades album merge from fuzzy-title to identifier-based. **[C]**
- **iTunes**: `amgArtistId` returned, unmapped. **[C]**
- **TheAudioDB**: `strMusicBrainzID` + a whole external-ID hub (`strDiscogsID`/`strSpotifyID`/`strItunesID`/`strUPCID`/…) on the searches we already run. **[C]**

This is the cheapest correctness win in the whole audit — the merge layer (`EntityResolutionTier`, ADR-0011) is being under-fed.

### 2.2 "Already fetched and discarded" — zero-extra-call enrichment
Beyond identity keys, several providers' richest data is in responses we already pay for:
- **Last.fm** `album.getInfo` returns the full tracklist; `lookupAlbumInfo` parses everything *except* `tracks[]`. **[C]**
- **Discogs** `tracklist[].extraartists[]` (per-track producer/engineer/feature credits — the headline "DAMN. track 1 → 9 credits" example) is flattened to one album-wide list; `release.notes` (liner notes) never decoded. **[C]**
- **Deezer** `/track/{id}` carries `contributors[]`/`available_countries` (deliberately excluded — sound call, already covered elsewhere). **[C]**
- **TheAudioDB** returns 8 artist art types + 6 album art types per call; we map 1 of each. **[C]**
- **Fanart.tv** returns ~8 art types; we map 3. **[C]**

### 2.3 The validator-vs-lister bug class
**MusicBrainz** is wired as a *validator* (`ValidateArtistAlbums`, which filters an input album list) but called with a `nil` seed, so it returns empty and casts **zero votes in the live consensus pipeline AND the diagnostic** (§3.4). It has a working discography lister (`fetchReleaseGroups`) that's never used as a contributor. This is a live product bug, not just a metric artifact.

### 2.4 Datacenter-IP fragility (the internal-API providers)
The keyless/internal-API providers (**YouTube Music** InnerTube, **SoundCloud** api-v2, **Deezer** pipe/gw-light) were all validated from a **residential dev IP**. Since Google's March-2025 BotGuard hardening, anonymous InnerTube reads from **datacenter IPs (the OCI prod VM)** increasingly 403. **The keyless assumptions must be re-validated from the prod IP before scaling any internal-API work.** The fix pattern already exists in-repo: the SoundCloud `client_id` self-heal; YT Music needs the analogous `visitorData` bootstrap.

### 2.5 The artwork chain is one ordered list applied to all kinds
`buildArtworkChain` ([search_wiring.go:158](../../services/go-api/internal/app/search_wiring.go#L158)) is kind-agnostic. This mis-orders **album** art: Fanart.tv (1000px) sits at #2 and can beat CAA (1200px) and iTunes (3000px master) purely by firing first. Per-kind ordering — or a kind-aware skip — is the one structural artwork fix worth considering. Artist ordering is already correct (CAA has no artist art, so Fanart is effectively primary there).

---

## 3. Per-provider findings

Effort: **S** = hours (map a field, lift a limit), **M** = a port + use case + cache, **L** = a custom client / new entity / acquisition plumbing.

### 3.1 Deezer (seed / primary search provider)
Public `api.deezer.com` (no auth) + the `pipe.deezer.com` anonymous-JWT lyrics path. Its doc says "fully maximized" — **true for enrichment depth, false for the discovery/recommendation surface.**

**Using today:** `/search/{track,album,artist}` (`order=RANKING`, `limit=15`), `/album/{id}/tracks`, `/artist/{id}/top`, `/artist/{id}/albums`, `/chart/0/*`, `/track/{id}` (isrc/bpm/gain/explicit), `/album/{id}` (upc/label/genres), pipe lyrics. Popularity signals `rank` (track) + `nb_fan` (artist; album returns 0 → positional fallback). **[C]**

**Untapped (ranked):**
| Move | Surface | Kind | Effort | Notes |
|---|---|---|---|---|
| `/artist/{id}/related` | similar-artist graph, keyless, `nb_fan` per neighbor | coverage/disc | **S** | Only have this from key-gated Last.fm today |
| `/artist/{id}/radio`, `/track/{id}/radio` | Deezer's recommendation-engine output (ranked tracks) | coverage | **S–M** | Cheapest stand-in for the deferred ML direction |
| Advanced `/search` filters | `bpm_min/max`, `dur_min/max`, `label:`, `order` variants | coverage | **S** | Feeds the BPM/ML-audio axis; new query modes, not new rank signals |
| `/editorial`, `/genre/{id}/*` | genre taxonomy + new-releases + per-genre radios | coverage | **M** | Speculative — needs a browse surface (YAGNI) |

**Skip:** `gw-light.php` internals and `pipe` beyond lyrics — high ToS/fragility; their real value (`song.getListData` stream-URL derivation) belongs to the **acquisition** roadmap, not discovery. **Open [I]:** lyrics adapter JWT field name + `writers` scalar-vs-array still unverified (deezer.md §8).

**Top 3:** `/artist/{id}/related` → `/artist/{id}/radio` → advanced search filters.

### 3.2 iTunes / Apple Music
Two keyless endpoints (`/search`, `/lookup`) + the mzstatic artwork transform. The 403 is the priority.

**The 403 — root-caused [C]:** the iTunes Search API enforces **~20 calls/min/IP → HTTP 403** when exceeded; the adapter has **zero throttling** while amplifying each query into 3 kind-searches + per-album consensus lookups. Album 403s worst because it's *last* in `defaultKindOrder` (artist, track, album) and eats the exhausted minute-budget. The default `Go-http-client` User-Agent makes Apple's heuristic stricter.

**Fix [C], effort S:** package-level shared `rate.Limiter` at ~17/min (`rate.Every(3500ms)`, burst 1) — *not* per-instance, since 4 iTunes adapters exist; call `Wait(ctx)` in `searchKind`/`Resolve`/`lookupContent`/`LookupAlbum` + set a real `User-Agent` (the `LookupAlbum` hand-rolled request bypasses `getJSON`, set it there too) + add `country=US`. Pure plumbing, off the ranking path.

**Untapped:**
| Move | Surface | Kind | Effort |
|---|---|---|---|
| `limit=15 → 200` | max is 200; zero extra calls, deeper recall | coverage | **S** |
| `media=music` + `attribute=` targeting | precision (stop track-named-after-artist pollution) | coverage | **S** |
| Map discarded fields | `copyright`, `trackNumber`/`discNumber`, explicit rating, `isStreamable`, **`amgArtistId`** | enrichment | **S** |
| Hero artwork → ~3000px | URL rewrite, master clamps automatically | enrichment | **S** |
| UPC/AMG bulk `/lookup` | batches many ids in 1 call (reduces rate pressure) | coverage | **M** |

**Skip:** Apple Music `amp-api` anonymous web-player token — Apple actively hardening (`root_https_origin`), highest-fragility, unlocks little we don't have keylessly. Official JWT is $99/yr — gate. **[I]:** `isrc` lookup param is NOT in Apple's documented keys — probe before relying.

**Top 3:** 403 fix → `limit=200` + map free fields → leave `amp-api` gated.

### 3.3 Last.fm (the coverage backbone — 9,937 unique entities)
Public API, `api_key` query param only. The backbone, and it's capped.

**The cap — [C]:** `GetArtistAlbums` hardcodes `artist.gettopalbums&limit=50` with no `page`. The API has **no documented max**; this truncates every artist's discography at 50 albums. Almost certainly a real chunk of the 15.2% gap, and it caps the source of two-thirds of the album union. It also drops the per-album `mbid` + `rank` it returns.

**Untapped:**
| Move | Surface | Kind | Effort |
|---|---|---|---|
| `limit=50 → 500` + paginate to `totalPages`; map `mbid`+`rank` | the cap fix | coverage | **S** |
| `artist.getCorrection` | canonical name + free MBID *before* merge → tighter dedup, fewer split entities | enrichment/identity | **S** |
| `album.getInfo` tracklist | already in the response you fetch, discarded | enrichment | **S** |
| `track.getSimilar` | mainstream similar-tracks rail (counterpart to SC underground) | coverage | **M** (needs surface) |
| `tag.gettopartists` / `geo.gettopartists` | genre + regional discovery reach (multi-region household) | coverage | **M** (needs surface) |

**[I]:** the "~5 req/s" limit is community lore, not in the ToS (which gives no number); there's also a 100 MB stored-data cap. Artist images are deprecated placeholders — keep MB-keyed.

**Top 3:** lift the 50-album cap → `artist.getCorrection` → map the album tracklist.

### 3.4 MusicBrainz (the dead contributor)
ws/2 API, 1 req/s limiter, MBID-keyed enrichment.

**The bug — [C]:** the `musicbrainz` ConsensusProvider in [search_wiring.go:100](../../services/go-api/internal/app/search_wiring.go#L100) calls `mb.ValidateArtistAlbums(ctx, artistName, nil)` and returns `.Confirmed`. `ValidateArtistAlbums` ([musicbrainz.go:301](../../services/go-api/internal/discovery/adapters/providers/musicbrainz.go#L301)) is a *filter* over an input album slice — `nil` input → empty output. So MB casts zero votes in the **live `ConsensusService`** (artist-detail albums) and scores 100% gap. It wastes an MBID-resolve + 100-RG browse, then discards everything.

**Two MB roles — don't conflate:** (1) **contributor** — the dead `BuildConsensusProviders` entry; (2) **authority** — `WithMBAuthority` / `applyMBAuthority` (contamination check) — **works, must not change.** The fix only touches role 1.

**Fix [C], effort S:** add an exported `ListArtistDiscography(ctx, artistName)` (reuses existing `resolveArtistMBID` + `fetchReleaseGroups` + `mapMBReleaseGroup`), swap the one fetcher line. **Risk:** MB now casts real votes → can flip albums `unconfirmed → confirmed` on artist detail (never *rejects* — rejection stays with the authority). Re-run signal-b + spot-check a few artists before merge.

**Untapped:**
| Move | Surface | Kind | Effort |
|---|---|---|---|
| Fix dead contributor (above) | 0 new calls | coverage | **S** |
| Paginate discography past 100 | `/release-group?artist={mbid}&limit=100&offset=N` | coverage | **S** |
| **ListenBrainz popularity** | `/1/popularity/*` batch POST by MBID — the listen-based popularity axis MB lacks | enrichment/ranking | **M** |
| Richer artist `inc=` | add `aliases`+`tags` to the lookup you already make (0 extra calls) | enrichment | **S** |
| More url-rel providers | parse Last.fm/AllMusic/Genius/Wikidata from `relations[]` already fetched | identity | **S** |
| Recording `inc=url-rels+isrcs` | track-level stream links + ISRCs (track identity bridge) | enrichment | **M** |

**Verdicts:** **AcousticBrainz is dead** (offline since 2022) — do not build on it. **ListenBrainz: yes** — live successor, MBID-native, no-auth reads, batch endpoints, separate rate limiter. **Local MB mirror (`musicbrainz-docker`): defer** until the 1 req/s wall actually saturates (post-pagination/ListenBrainz); then it pays for itself immediately.

**Top 3:** fix dead contributor → ListenBrainz popularity → paginate discography + enrich url-rels/aliases in place.

### 3.5 YouTube Music (works, but adds the least unique reach — 582)
`raitonoberu/ytmusic` wrapper → **one** InnerTube endpoint (`/youtubei/v1/search`), keyless via a static web-player key. **One unfiltered search, page 1 only**, bucketed client-side. No `browse`, no pagination, no per-kind filter (the lib can't). No ISRC/MBID emitted → no identity-bridge contribution. **[C]**

**⚠️ Validate keyless from the prod OCI IP first (§2.4)** — the doc's "keyless works" was a residential-IP probe.

**Untapped:**
| Move | Surface | Kind | Effort |
|---|---|---|---|
| Per-kind `params` filter token | albums-only full shelf (`params="EgWKAQIYAWoMEA4QChADEAQQCRAF"`) — hits the album gap | coverage | **S** |
| Search continuation (page 2+) | re-POST `/search` with `continuation` ctoken | coverage | **S–M** |
| Custom `youtubei/v1/browse` artist client | full discography (singles/EPs/videos/regional the search path drops) | coverage | **L** |
| `GetWatchPlaylist` related | ~50 related tracks (lib already exposes) | enrichment | **S** (needs surface) |
| `visitorData` bootstrap | self-heal for datacenter-IP blocking | resilience | **M** |

**Skip/retire:** the key-gated Data API v3 (`youtube.go`) is quota-crippled and adds nothing InnerTube can't — candidate for retirement once keyless artwork is confirmed on prod. **Reprioritized:** given only 582 unique reach, the L-effort browse client (B3) is **lower priority than the iTunes 403 fix.**

**Top 3:** validate prod-IP keyless (+`visitorData` if degraded) → per-kind filter + continuation → defer the custom browse client.

### 3.6 SoundCloud (the underground long tail)
Two adapters: `api-v2` (primary, `client_id` self-heal) + a yt-dlp fallback (track-only). The internal api-v2 is the only viable path (official OAuth API is closed to new apps). **[C]**

**Using today:** `/search/tracks` (paginated to 40), `/search/albums`, `/search/users`, `/resolve`, `/users/{id}/toptracks`, `/users/{id}/albums`, `/tracks/{id}/related`. Carries `genre`/`likes`/`reposts` in extras (unused); ranks on `duration` + `playback_count`. **[C]**

**Untapped:**
| Move | Surface | Kind | Effort |
|---|---|---|---|
| Map `publisher_metadata.isrc` | already in every track object — promotes SC to identity participant (§2.1) | enrichment/identity | **S** |
| `/users/{id}/tracks` chronological + `linked_partitioning` | the zero-play just-dropped uploads `toptracks` structurally can't return | coverage | **S** |
| Deeper track-search paging | `limit=200` + follow `next_href` past 40 | coverage | **S** |
| `media.transcodings` → progressive MP3 | exact-track audio for acquisition (Unit D, code-complete/unverified) | acquisition | **L** |
| `/charts?kind=trending&genre=` | genre browse/discovery feed | coverage | **M** (needs surface) |
| `/tracks/{id}` full metadata | `tag_list`, `label_name`, `purchase_url`, description | enrichment | **M** |

**[I]:** `/stations/track:{id}/tracks` autoplay radio (deeper related seed) — probe; `playback_count`-into-ranking needs the eval gate.

**Top 3:** map `publisher_metadata.isrc` → `/users/{id}/tracks` + deeper paging → finish `media.transcodings` acquisition (verification-bound).

### 3.7 Discogs (credits/styles/labels authority)
Token auth, ~1 req/s hand-rolled limiter, detail-open only. Caps 1–7 all wired.

**Using today:** `/database/search` (artist + master resolve), `/artists/{id}`, `/artists/{id}/releases`, `/masters/{id}`, `/releases/{id}` (the master's main_release). **[C]** Fetched-but-dropped: `master.images/videos`, `release.identifiers[]`, `release.notes`, `release.series[]`.

**Untapped:**
| Move | Surface | Kind | Effort |
|---|---|---|---|
| Per-track `extraartists[]` from `/releases/{id}` | the concrete edition's per-song credits (currently flattened to album-wide) | enrichment | **S** |
| `release.notes` + `series[]` | liner notes (strip BBCode w/ existing cleaner) + box-set grouping | enrichment | **S** |
| `release.identifiers[]` barcode → merge key | identifier-based album merge (§2.1); unlocks `?barcode=` deterministic resolve | identity/coverage | **S→M** |
| `/masters/{id}/versions` | editions list ("which pressing"); substrate for richer-release credit selection | coverage | **M** |
| `/database/search?credit=` / `?style=&year=` | "everything producer X worked on" / style-era browse | coverage | **M** (needs surface) |
| `/labels/{id}` + releases | label browse — new entity | coverage | **L** (weak product pull) |

**Skip:** marketplace price-suggestions (seller-settings gated, irrelevant to streaming).

**Top 3:** per-track credits + liner notes (free) → barcode for identifier-based merge → `/masters/{id}/versions` editions view.

### 3.8 TheAudioDB (demote to artwork-by-identity)
Project key `523532` is **just another free test key** (1-result artist-search cap, v1 only, 30/min). **[C]** Currently maps 3–4 fields (`strArtistThumb`, `strAlbumThumb`) from 2 endpoints.

**Verdict: demote from search provider to a pure artwork-by-identity resolver.**
- **As coverage:** useless — 1-result cap + exact-name requirement fail the ambiguous-query hard case; no ranking signal. Consider **dropping it from the consensus/search set** ([app.go:439](../../services/go-api/internal/app/app.go#L439)).
- **As artwork:** uniquely valuable — the **only** source for transparent artist logos (`strArtistLogo`), `clearart`, `banner`, `widethumb`, `fanart` backgrounds, and album `cdart`/`spine`/`back`. All already in the responses we fetch.

**Untapped (all [C], free key):**
| Move | Surface | Effort |
|---|---|---|
| Harvest full artwork set (E1+E2) | 8 artist + 6 album art types on calls we already make | **S** |
| `artist-mb.php?i={mbid}` lookup | deterministic by MBID instead of name-fuzzing (the `Resolve` mbid arg is currently ignored) | **S** |
| External-ID hub + music videos (`strMusicVid`) | identity bridge + the one non-duplicative axis (videos) | **M** (needs spec) |

**Do not pay for premium** — everything useful is on the free key.

**Top 3:** harvest the full artwork set → switch to MBID lookup → (deferred) external-ID hub / music videos behind a spec.

### 3.9 Cover Art Archive (front-of-chain, correctly)
One call: `HEAD /release-group/{mbid}/front-500`, redirect-followed to archive.org. Release/album only (no artist art). Unmetered (Internet Archive), MBID-gated. **[C, live-probed]**

**Untapped:**
| Move | Surface | Effort |
|---|---|---|
| Hero → `front-1200` (or `/front` full original) | same path, same redirect, confirmed live | **S** |
| Per-release `/release/{mbid}` JSON gallery | back/booklet/gatefold/disc faces — the one art axis only CAA owns (needs release MBID; RG JSON's 307 names it) | **M** |
| Filter on `approved`/`front` flags | quality selection, rides on the gallery JSON | **S** |

**Verdict:** front-of-chain is **correct** (MB-native, highest-fidelity, original-res). Album-art-only; self-skips for artist kind (right). Depth wins are in *what* we pull (1200px + gallery), not *where* it sits.

**Top 3:** bump hero to 1200px → per-release gallery for detail screens → approved/front filtering.

### 3.10 Fanart.tv (artist-image authority + backdrops)
`/v3/music/{mbid}` (artist) + `/v3/music/albums/{rg-mbid}` (album), `api_key` only, MBID-gated, #2 in the chain. Maps 3 of ~8 art types. **[C, live-probed]**

**Untapped:**
| Move | Surface | Effort |
|---|---|---|
| `artistbackground` (1920×1080) as its own detail backdrop slot | nothing else in the chain has a 16:9 artist backdrop; currently wasted as a square-thumb fallback | **S** |
| Use a **personal key** as `api_key` (or add `client_key`) | bypasses project rate limits + sees newer art; fits self-hosted doctrine | **S** |
| Rank by `likes`, prefer `lang` instead of `[0]` | better image selection, 0 new calls | **S** |
| `cdart` (1000px transparent disc art) | physical-media texture for album detail | **S–M** |
| `hdmusiclogo`/`musiclogo` transparent wordmarks | premium detail-header overlay | **M** (needs UI slot) |

**Chain mis-ordering [C]:** Fanart album covers are **1000px — below CAA (1200) and iTunes (3000-master)**. For the **album** kind, Fanart should rank below CAA/iTunes (coverage fallback, not quality source); for **artist** it's correctly #2 (CAA has no artist art). **Doc correction:** `musicbrainz.md` cap 6 implies HD album covers — the "HD" claim holds for *artist* art only.

**Top 3:** promote `artistbackground` to a backdrop slot → switch to a personal key → `likes`/`lang` selection.

### 3.11 Genius (promote from artwork-only to credits + relationships)
Currently **artwork-only**: one `GET /search` call mapped to `song_art_image_url`/`header_image_url`, wired as a low-priority chain fallback. The authenticated `/songs/{id}` + `/artists/{id}` endpoints are never called. **[C]**

**Untapped:**
| Move | Surface | Kind | Effort |
|---|---|---|---|
| `/search` → `/songs/{id}` per-song credits | `producer_artists`/`writer_artists`/`featured_artists` — the only **song-level** credit source (Discogs is album-level) | enrichment | **M** |
| `song_relationships[]` | samples/interpolates/covers/remixes — a discovery axis **no other provider has** | discovery | **M** (needs spec) |
| `media[]` external links | Spotify/YT/Apple/SC canonical links (mostly duplicative) | enrichment | **S** |

**Verdict: yes — promote to a `GeniusEnricher` (credits + song relationships), but NOT a lyrics source.** Lyrics are scrape-only (not in the API), HTML-selector-fragile, and carry **publisher-copyright legal exposure** beyond the project's provider-ToS posture — and Deezer already owns synced lyrics. **[I]:** exact `song_relationships[].relationship_type` enum strings + payload shapes need a live probe with a real token.

**Top 3:** `GeniusEnricher` for per-song credits → fold in `song_relationships` (behind a spec) → do **not** build lyrics scraping.

---

## 4. Consolidated prioritized backlog

### Tier 0 — coverage/correctness bugs (S, do first, highest leverage)
1. **iTunes 403 fix** — shared `rate.Limiter` ~17/min + real User-Agent + `country=US`. Unblocks ~half of iTunes (1,704+ unique reach). Off ranking path.
2. **Last.fm 50-album cap** — `limit=500` + paginate; map `mbid`+`rank`. Un-caps the backbone (66% of the union).
3. **MusicBrainz dead contributor** — `ListArtistDiscography` + one wiring line. Revives MB in live consensus. *Needs signal-b rerun + artist spot-check (changes detail-screen album lists).*

These three widen the search/consensus union → **must clear `discoveryeval -mode eval -top-k 3`** (baseline 1773/1792) before trusting.

### Tier 1 — zero/low-cost enrichment & identity (S)
- Identity keys already fetched: SoundCloud `isrc`, Discogs barcode, iTunes `amgArtistId`, TheAudioDB external-ID hub.
- Already-fetched data discarded: Last.fm album tracklist, Discogs per-track credits + liner notes, iTunes free fields.
- Last.fm `artist.getCorrection` (name normalization + free MBID).
- iTunes `limit=200`; CAA hero `front-1200`; Fanart `artistbackground` backdrop + personal key + `likes`/`lang`; TheAudioDB full artwork set + MBID lookup.

### Tier 2 — new capabilities (M, mostly need a `/feature-spec` surface)
- Deezer `/artist/{id}/related` + `/artist/{id}/radio` (recommendation surface; cheapest ML stand-in).
- ListenBrainz popularity by MBID (the popularity axis MB lacks).
- Genius `GeniusEnricher` (per-song credits + song relationships).
- Discogs `/masters/{id}/versions` editions + barcode-keyed resolve.
- CAA per-release gallery; SoundCloud charts; Last.fm `track.getSimilar`/`tag`/`geo`.

### Tier 3 — heavy / gated (L)
- SoundCloud `media.transcodings` acquisition (verification-bound; product-vision payoff).
- YouTube Music custom `browse` discography client — **deprioritized** (only 582 unique reach; validate prod-IP keyless first).
- Local MusicBrainz mirror (defer until 1 req/s saturates).

### Explicitly not recommended
- Apple Music `amp-api` anonymous token (hardening + low marginal value).
- Genius lyrics scraping (publisher-copyright risk; Deezer owns lyrics).
- Deezer `gw-light`/`pipe` beyond lyrics for discovery (belongs to acquisition).
- TheAudioDB premium key; TheAudioDB as a search provider; Discogs marketplace.

---

## 5. Validation still owed (live probes before building)
- **Prod-IP keyless check** for YouTube Music / SoundCloud / Deezer-pipe from the OCI VM (§2.4) — gates all internal-API work.
- **Genius** `/songs/{id}` payload + `song_relationships` enum strings (no token in hand this session).
- **iTunes** `isrc` lookup param support; the ~17/min limiter target (Apple exposes no rate-limit headers).
- **Deezer** lyrics JWT field name + `writers` scalar-vs-array (deez.md §8, still open).
- **Discogs** B1/B8 (barcode bridge value; better-release-for-credits heuristic).

## 6. Method & provenance
Per-provider research agents read the live adapter code + provider doc + did web research (official docs, reverse-engineering writeups, client-library mirrors); several live-probed (CAA, Fanart.tv, TheAudioDB, iTunes artwork). Coverage numbers are from `discoveryeval -mode signal-b` against prod, 311 artists, with the unique-reach measure added this session. Provider docs (`docs/providers/*.md`) were cross-checked and corrected where drifted (Fanart "HD album" claim; Deezer "fully maximized" scope; YT "keyless works" IP caveat). No provider docs exist yet for TheAudioDB, Fanart.tv, Cover Art Archive, or Genius — candidates to author from these sections.
