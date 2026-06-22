# MusicBrainz enrichment

> Spec for `musicbrainz-enrichment` — version 1, drafted 2026-06-22.
> Authors: solo + Claude.
> Status: Planned (spec-reviewer + plan-reviewer 2026-06-22: revise-then-approve → revised; plan.md ready)

## Problem

A discovery result carries only what the source provider returned — usually a title, an artist
subtitle, and whatever thumbnail that provider had. Opening a result's detail screen shows no genres,
no release year, no community rating, and frequently a low-resolution or wrong-artist cover, because
the result has no MusicBrainz id to key the good data off. MusicBrainz exposes all of this for free —
curated genres, ratings, authoritative release year, and the cross-provider id graph — and Cover Art
Archive serves 1200px MBID-keyed covers, but today none of it is wired (the adapter only does name
search + identity/consensus). The audit (`docs/providers/musicbrainz.md` §5/§8) verified the surface
live; this spec wires it.

## User value

Opening a result's detail screen now shows **what it actually is**: its genres (e.g. *conscious hip
hop, jazz rap, west coast hip hop*), its release year, a community rating when one exists, and a
**sharp, correct cover** — even for a result that arrived from a provider (Deezer, iTunes) that had no
MBID and only a small thumbnail. The detail screen stops looking like a bare search row and starts
looking like a record.

## Scope tier / MVP cut

Right-size to the project stage. **Default for this solo, pre-launch app: the minimal tier.**

- **Minimal (ship this):** a read-only **detail-open** enrichment endpoint
  `GET /discovery/enrichment?kind=&title=&subtitle=&mbid=` that, for one entity, resolves its
  MusicBrainz id (uses a passed `mbid`, else strict name-resolves one), does a single MB lookup with
  `inc=genres+ratings+url-rels`, runs the **existing artwork chain** with that MBID, and returns
  `{ mbid, genres[], year, rating, rating_votes, primary_type, secondary_types[], external_ids{}, artwork_url }`.
  Read-through cached by `(kind, mbid)` — the **whole** value object, `artwork_url` included. Mobile:
  a `useEnrichment` hook (fetch on detail open) that renders a genres/year/rating block and upgrades
  the detail artwork. Off the ranking path.

**DTO invariants (apply to every AC):** `genres`, `secondary_types`, `external_ids` are always
present (`[]`/`[]`/`{}`), never null. `external_ids` keys are lowercase provider names
(`deezer`, `spotify`, `discogs`, `wikidata`); **values are the bare extracted id**, not the URL
(`"525046"`, `"Q130798"`, `"1539549"`) — the last non-empty path segment of the relation URL. For
`kind=artist`, the album-only fields are zero-valued: `year:0`, `primary_type:""`, `secondary_types:[]`.
- **Deferred to post-launch:**
  - **Search-path enrichment** — enriching the results *list* / search-card thumbnails. That means
    blocking the hot search path on MB's 1 req/sec wall (or a background cache-warm queue) and, for
    non-MB results, attaching MBIDs by name match — which changes search-card artwork and therefore
    needs the `discoveryeval --top-k 3` gate. Detail-open sidesteps all of it.
  - **Identity-based merge (the cross-provider bridge as a *merge* input)** — using the url-rel
    Deezer/Spotify/Discogs ids to merge provider results by identity instead of name similarity.
    Changes merge → ranking → eval-gated. This spec *extracts and returns* those ids (useful on the
    detail screen now, and seeds the future work) but does **not** feed them into `Merge`/`Rank`.
  - **Feeding genres/rating into ranking.** Display-only here.
  - **Fanart.tv field-shape work** (needs `FANARTTV_API_KEY`); it already sits in the artwork chain
    and benefits from the resolved MBID for free.
- **Justified exceptions:** the read-through cache is pulled into the minimal tier — **needed now
  because** MB's hard 1 req/sec limit makes an uncached per-open lookup a self-inflicted latency/limit
  problem, and the cache adapter is a ~30-line mirror of the existing `RedisArtworkCache`.

The Acceptance criteria below cover the **minimal tier only**.

## Acceptance criteria

Each one is testable. Each one will become at least one automated test.

1. **AC#1 (artist enrichment mapping)** — Given an artist MBID, when the MB lookup returns
   `genres[]`/`tags[]`/`rating`/`relations[]`, then enrichment exposes the **curated `genres`**
   (from the `genres[]` field, not `tags[]`; deduped; ordered by vote count descending, **ties broken
   alphabetically** so the order is deterministic), `rating`+`rating_votes`, and `external_ids` with
   the bare ids for `deezer`, `spotify`, `discogs`, `wikidata` (other relation types ignored, not
   errored). Artist DTO has `year:0`, `primary_type:""`, `secondary_types:[]`.
2. **AC#2 (album enrichment mapping)** — Given a release-group MBID, when looked up with
   `inc=genres+ratings`, then enrichment exposes `genres` (same dedup/sort as AC#1), `year` parsed
   from `first-release-date` (4-digit prefix; `0` when absent/malformed), `primary_type`
   (Album/EP/Single), and `secondary_types` (e.g. Compilation/Live; `[]` when none).
3. **AC#3 (MBID resolution fallback)** — Given **no** `mbid` but a valid `kind`+`title` (+`subtitle`),
   when enriched, then the service resolves an MBID by taking the **first** MB search candidate whose
   `NormalizeForMatch(title)` equals the normalized query title **and**, when `subtitle` is non-empty,
   whose normalized primary artist-credit equals the normalized subtitle (for `kind=artist` the match
   is title-only). If no candidate matches, it returns **empty enrichment** (200, `mbid:""`, empty
   lists/maps) — never a wrong-entity guess and never an error.
4. **AC#4 (artwork via chain)** — Given a resolved/passed MBID, when enriched, then the **existing
   `ArtworkResolver` chain** is consulted with that MBID and `artwork_url` is its result (so an
   album with a Cover Art Archive cover returns the HD URL; a miss returns `""`).
5. **AC#5 (read-through cache, whole value)** — Given two identical enrichment requests, when the
   second runs, then it is served from cache and issues **neither** a second MB lookup **nor** a second
   artwork-chain resolve (the cached value is the complete `MBEnrichment`, `artwork_url` included);
   verified via call-counting fakes for both the enricher and the resolver. A request that resolves to
   **no** MBID is negatively cached on `(kind, normalized title+subtitle)` so a repeat does not
   re-run the name resolution.
6. **AC#6 (graceful degradation)** — Given MB returns an error, times out, returns a non-200, or the
   passed `mbid` 404s, when enriched, then the endpoint returns **200 with empty enrichment** and
   never fails the request (mirrors the artist/album/related content endpoints'
   `ProviderStatusError`-as-200 contract).
7. **AC#7 (endpoint validation)** — Given a request with a missing/blank `kind`, then `400`; given an
   unknown `kind`, then `400`; given a missing/blank `title` with no `mbid`, then `400`; given a valid
   request, then `200` with the enrichment DTO (honoring the DTO invariants above).
8. **AC#8 (mobile: gated render + artwork upgrade)** — Given a detail screen mounts, when enrichment
   returns genres/year/rating, then a metadata block renders them; when enrichment is empty or errors,
   the block is hidden and the screen is otherwise unaffected; when `artwork_url` is non-empty, the
   detail artwork upgrades to it.

## Out of scope

- **Search results list / search-card** enrichment and thumbnail upgrades (deferred tier).
- **Changing `Merge` or `Rank`** in any way — no eval-gated behavior. external_ids are returned but
  not fed into merging.
- **Genre/rating as ranking signals.**
- **Writing enrichment onto the `Track` aggregate** / persistence — this is a live read surface only.
- **"Open in Spotify/Deezer" deep links** in the UI — the ids are returned, but rendering link
  buttons is a later UI increment; the minimal mobile surface renders genres/year/rating only.
- **Recording (track) lookups beyond what name-resolution needs** — tracks already carry ISRC from
  search; the rich detail surface that matters first is artist + album.

## Design considerations

Vault lookup (per `.claude/rules/vault-consultation.md`):
- [vault: wiki/concepts/Lazy Initialization.md] — the enrichment is **lazy / load-on-first-access**:
  the expensive MB+artwork resolution is deferred to detail-open (not paid on every search) and
  memoized by MBID, the note's exact "Ghost / Partial Loading" + read-through shape. The 1 req/sec
  wall is precisely the "first-call latency spike" trade-off the note flags → mitigated by detail-open
  placement + cache.
- [vault: wiki/concepts/Proxy Pattern.md] — the read-through cache is a caching proxy in front of the
  MB lookup (cache lookup → real subject on miss).
- `vk_search "read-through cache enrichment external API aggregation"` returned only tangential hits
  (API Gateway, Observability) — **vault returned no dedicated content-enricher note**; the feature is
  small and read-only, so no pattern is stretched.

High-level approach (not implementation detail — that's the plan):

- This is a **read** path in the `discovery` bounded context, structurally identical to the
  artist-content / related-tracks endpoints: handler route → service → a new outbound port
  (`MetadataEnricher`) implemented by the MusicBrainz adapter, with a read-through cache port.
- It needs a **new value object** (`MBEnrichment`, immutable) and **two new ports** (`MetadataEnricher`,
  `EnrichmentCache`) — no new aggregate. Enrichment is not persisted.
- It introduces **no new external dependency**: MusicBrainz and the Cover Art Archive resolver are
  already integrated; this wires their unused surface. No ADR required.
- **Off the ranking path** (like related tracks / artist discography): display-only enrichment on
  detail-open, so **no eval gate** is required.

## Dependencies

- **Bounded contexts**: `discovery` (existing).
- **Other features**: the detail screen (`view-result-detail`) hosts the metadata block; `related-tracks`
  is the structural precedent for the detail-open fetch + hook + gated render.
- **External services**: MusicBrainz `ws/2` and `coverartarchive.org` (both already integrated).
- **Library/framework additions**: none (chi, TanStack Query, go-redis already in use).

## Risks / open questions

- **Risk: MB 1 req/sec wall.** An uncached open costs 1–2 serialized MB calls (~1–2s). Mitigation:
  detail-open placement (not the search path), read-through cache (14-day positive TTL), client
  `staleTime` + its own timeout, best-effort degradation (AC#6). The MB adapter's existing global
  `rateLimit()` serializes concurrent opens safely.
- **Risk: wrong-entity name resolution** when no `mbid` is passed. Mitigation: **strict** normalized
  title(+artist) equality (reuse `textnorm.NormalizeForMatch`, the same gate the identity resolver
  uses), and return empty rather than a fuzzy guess (AC#3). Detail-open is user-initiated on one
  entity, so blast radius is one screen, never search ordering.
- **Resolved — cache key.** Positive hits key by the resolved `(kind, mbid)` and store the whole
  `MBEnrichment`. A request that resolves to no MBID is cached negatively on
  `(kind, normalize(title+" "+subtitle))` with a shorter TTL (AC#5). Two-tier TTL mirrors
  `RedisArtworkCache` (14-day positive / 24-hour negative).
- **Resolved — `external_ids` value shape.** Bare extracted id (last non-empty path segment of the
  relation URL), keys lowercase provider names. See DTO invariants.
- **Open question: how many genres to surface in the UI.** Backend returns all curated genres ordered
  by vote count (ties alphabetical); UI caps the rendered set at the **top 4**. Confirmed in the
  mobile slice; trivially adjustable.

## Telemetry

- **Log events**: enrichment lookup at the service layer — `kind`, resolved `mbid` (or "unresolved"),
  genre count, artwork hit/miss, cache hit/miss, MB latency — structured `slog`, correlation id.
- **Metrics**: enrichment cache hit rate, MB lookup latency, artwork-upgrade rate (fraction of opens
  that return a non-empty `artwork_url`), unresolved-MBID rate. Deferred-but-noted; not blocking.
- **Alerts**: none pre-launch.

## Related

- `docs/providers/musicbrainz.md` §4 (verified endpoint catalog), §5 (capabilities 3/4/5), §6 (the
  1 req/sec cost), §8 (next-steps order this spec implements).
- Predecessor specs: `docs/specs/related-tracks/spec.md` (detail-open fetch + hook + gated-render
  precedent), `docs/specs/view-result-detail/spec.md` (the detail screen host),
  `docs/specs/discovery-identity-v1/spec.md` (`SearchResult`/`SourceRef`/`extras["mbid"]` shape).
- Vault: [vault: wiki/concepts/Lazy Initialization.md], [vault: wiki/concepts/Proxy Pattern.md].
