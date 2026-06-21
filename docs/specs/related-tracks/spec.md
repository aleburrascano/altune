# SoundCloud related tracks

> Spec for `related-tracks` — version 1, drafted 2026-06-21.
> Authors: solo + Claude.
> Status: Shipped (2026-06-21 — backend + mobile built and tested; see plan.md)

## Problem

When a user opens a track's detail screen, the only way to keep discovering is to
go back and search again. SoundCloud's internal API exposes a per-track
recommendation set (`/tracks/{id}/related`) that surfaces the underground long
tail — leaks, collabs, and remixes that pure keyword search misses (a live probe
turned up "Lil Tecca & Ken Carson – Fell In Love" off a single seed track). Today
that signal is reachable but unused: there is no surface that says "more like
this."

## User value

Opening a SoundCloud-sourced track now shows a **"Related on SoundCloud" rail** of
tappable tracks. The user can keep pulling the thread — seed → related → its
related — without returning to search, and the rail reaches exactly the
underground material the mainstream catalogs don't index.

## Scope tier / MVP cut

Right-size to the project stage. **Default for this solo, pre-launch app: the minimal tier.**

- **Minimal (ship this):** a read-only `GET /discovery/tracks/{provider}/{externalId}/related`
  endpoint backed by SoundCloud's `/tracks/{id}/related`, and a horizontal
  "Related on SoundCloud" rail on the **track** detail body that fetches on
  detail open (TanStack Query) and is shown **only** for results carrying a
  SoundCloud source. Raw SC set, mapped through the existing track mapper, no
  re-ranking or dedup.
- **Deferred to post-launch:** merging SC related with other providers' related
  signals; ranking/dedup of the merged set; caching beyond the client query
  cache; related for album/artist details; inline-in-results placement;
  prefetching during search.
- **Justified exceptions:** none.

The Acceptance criteria below cover the **minimal tier only**.

## Acceptance criteria

Each one is testable. Each one will become at least one automated test.

1. **AC#1 (endpoint, happy path)** — Given a SoundCloud track id, when
   `GET /discovery/tracks/soundcloud/{externalId}/related` is called, then it
   returns the SoundCloud related set mapped to `SearchResult`s of `kind=track`,
   each carrying a SoundCloud `SourceRef` (numeric id, permalink url), bounded to
   a server-side related limit (default 20; named constant, confirm value in plan).
2. **AC#2 (mapper reuse)** — Given the SoundCloud related response, when mapped,
   then each item is produced by the existing `mapSoundCloudAPITrack` (same
   `extras`: genre, playback/likes/reposts), and unmappable items are dropped, not
   surfaced.
3. **AC#3 (unsupported provider)** — Given a `provider` other than `soundcloud`,
   when the related endpoint is called, then the request still returns 200 with an
   empty `Items` set and `ProviderStatusError` — it does not fail the request,
   exactly mirroring the existing `GetArtistContentService` unknown-provider
   fallback (`get_artist_content.go`).
4. **AC#4 (rail gating)** — Given a track detail for a result that carries **no**
   SoundCloud source, when the screen mounts, then **no** related fetch is issued
   and **no** rail renders.
5. **AC#5 (rail render)** — Given a track detail for a SoundCloud-sourced result,
   when the screen mounts, then the rail fetches once and renders the related
   tracks as tappable cards below the Play/Save actions.
6. **AC#6 (empty set)** — Given the related endpoint returns an empty set, when
   the detail renders, then the rail is hidden entirely (no empty-rail shell).
7. **AC#7 (graceful failure)** — Given the related endpoint errors, times out, or
   the `client_id` is mid-rotation **after the resolver's retry is exhausted**,
   when the detail renders, then the rail is hidden and the rest of the detail
   screen is unaffected (no crash, no blocking spinner). (A successful re-resolve
   produces a populated rail — that is AC#5, not this case.)
8. **AC#8 (lateral navigation)** — Given a related-track card, when tapped, then
   the app navigates to that track's detail (reusing the existing lateral-nav
   path), making the rail chainable seed → related → related.

## Out of scope

Explicit non-goals. Things people might assume but we're not doing:

- **Merged / multi-provider related** — blending SC related with Deezer/iTunes/
  library signals, and any ranking or dedup of a merged set. Deferred tier.
- **Reusing `FindRelatedService` / `RelatedGroup`** — that existing surface is
  album-relationship + library-match grouping computed over *search* results;
  this feature is a distinct **track-keyed** detail-screen endpoint and does not
  fold into it.
- **Related on album or artist details** — track detail only.
- **Inline-in-results placement** and **search-time prefetch**.
- **Audio acquisition** of related tracks (that is Unit D, `acquire-track`).
- **Server-side caching / persistence** of related sets.

## Design considerations

Vault lookup (per `.claude/rules/vault-consultation.md`): `vk_search` for
"recommendation related content read path" returned only tangential matches
(DDD Advanced Patterns, Enterprise Integration Patterns) — **vault returned no
strong match for the recommendation / related-content read-path topic**. The
feature is small and read-only, so no pattern is being stretched.

High-level approach (not implementation detail — that's the plan):

- This is a **read** path in the `discovery` bounded context. It follows the
  established artist-content / album-tracks wiring exactly:
  handler route → service → provider port → SoundCloud adapter, with the adapter
  calling `/tracks/{id}/related` and reusing `mapSoundCloudAPITrack`.
- It requires a **new read port method** (track-keyed related), no new aggregate
  or value object — related items are ordinary `SearchResult`s.
- It introduces **no new external dependency**: the SoundCloud api-v2 client
  (`soundcloud_apiv2.go`) and its self-healing `client_id` resolver already exist;
  the adapter method is the ~20-line addition the provider doc (§5.5) describes.
- **Off the ranking path** — like artist discography (capability 4), the rail is
  display-only enrichment, labeled "Related on SoundCloud." No eval gate is
  required because nothing competes in `fuse_and_rank`.
- **Gating is intrinsic**: `/tracks/{id}/related` needs a SoundCloud numeric track
  id, which only exists in the `SourceRef` of SoundCloud-sourced results. The rail
  therefore appears only for those tracks by construction.

## Dependencies

- **Bounded contexts**: `discovery` (existing).
- **Other features**: detail screen (`view-result-detail`) for the rail host;
  the lateral-nav path it reuses already ships there.
- **External services**: SoundCloud api-v2 (already integrated, capabilities 1–4).
- **Library/framework additions**: none (TanStack Query, chi already in use).

## Risks / open questions

- **Risk**: `client_id` rotation breaks the endpoint mid-session — mitigation:
  the existing self-healing resolver re-resolves on 401/403, and AC#7 makes the
  rail degrade silently if it's still down.
- **Risk**: SoundCloud rate limits — mitigation: one bounded call per detail open,
  cached by TanStack Query key; no prefetch, no concurrent fan-out.
- **Open question**: is the raw SC ordering good enough, or does the rail need
  light hygiene (drop the seed track itself, de-dup obvious repeats)? — to resolve
  via: manual spot-check against a few seeds during implementation; add only if
  the raw set is visibly poor.
- **Open question**: a result could in principle carry more than one SoundCloud
  source — pick the first SC `SourceRef`'s id; confirm during the plan.

## Telemetry

- **Log events**: related-fetch (provider, seed track id, result count, provider
  status) at the service layer — structured `slog`, correlation id per request.
- **Metrics**: rail render rate (fraction of track-detail opens that show a rail),
  SC related latency, error/empty rate. Deferred-but-noted; not blocking.
- **Alerts**: none pre-launch.

## Related

- `docs/providers/soundcloud.md` §5.5 (capability 5), §8 (Unit C) — endpoint shape
  and field dumps this spec consumes.
- Predecessor specs: `docs/specs/view-result-detail/spec.md` (rail host),
  `docs/specs/discovery-identity-v1/spec.md` (SearchResult / SourceRef shape).
- Vault: no strong match (read-path, see Design considerations).
