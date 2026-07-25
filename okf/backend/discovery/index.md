---
type: Index
title: Discovery subsystems
description: The discovery bounded context decomposed into its ten subsystems — search orchestration through ranking, artist-detail discography, enrichment, telemetry, and the offline eval harness.
tags: [index, discovery]
---

Discovery is the multi-provider search context (`services/go-api/internal/discovery/`, ~18K LOC — 62% of the backend). A query flows: scatter-gather fan-out → identity stamping → merge/dedup → ranking → (on detail-open only) enrichment. Everything rank-affecting is eval-gated.

## Query path (in flow order)

- [scatter-gather](scatter-gather.md) — the Service facade fanning a query across providers in parallel
- [identity-artwork](identity-artwork.md) — durable EntityIdentity bridge + identity-first artwork chain for same-name entities
- [merge-dedup](merge-dedup.md) — identity-tiered entity resolution, artist consensus, disambiguation
- [ranking](ranking.md) — parameter-free relevance scoring, eval-gated tail-demotion and cross-kind prominence, diversity reshaping
- [query-correction](query-correction.md) — "did you mean", query cleanup, phonetics, autocomplete
- [vocabulary](vocabulary.md) — learned term-frequency store backing correction and suggest

## Off the query path

- [artist-detail](artist-detail.md) — the detail-open discography/top-tracks path: identity-first content fan-out by each provider's own id, the V2 best-of-merge → keep → MB-verify → bucket cores, and native album tracklists
- [enrichment](enrichment.md) — detail-screen MusicBrainz/Discogs/Last.fm/Deezer metadata + lyrics, never on the ranking path
- [cache-layer](cache-layer.md) — Redis read-through caches (no-op when Redis is absent); caches are app-wide, not per-user
- [telemetry](telemetry.md) — InteractionEvent pipeline + SatisfactionConsumer turning play/skip/completed into a ranking signal
- [eval-harness](eval-harness.md) — offline discoveryeval CLI measuring ranking/merge/diversity/coverage against committed baselines

The Mission Control operator console that displays discovery's telemetry is its own module — see [admin](../admin/index.md); discovery feeds it through consumer-defined seams and never imports it.

## Ownership stamping, blended slate, artist content (2026-07-25)

Three shapes moved here from the mobile client.

**Ownership stamping.** A discovery result now carries whether the signed-in user already owns it: `extras.owned_track_id` and `extras.owned_acquisition_status`, stamped on every track result the wire emits (search, album tracks, artist top-tracks, related, artist content). Before this, the client answered "do I own this?" by matching normalized title+artist against a full in-memory copy of the library — the single biggest reason the phone held every row it owned.

The seam is `ports.OwnershipReader`, implemented by `adapters/catalogbridge/ownership_reader.go` against a narrow local interface over catalog's repo — the same shape `playback/adapters/catalogbridge` uses, and the reason discovery never imports catalog's service layer. `ports.OwnershipKey` (`NormalizeForMatch(title) | NormalizeForMatch(artist)`) is the one normalizer both sides use; keeping it in `ports` is what stops the bridge and the wire stamper drifting apart. Stamping happens at the handler, never in the pipeline: ranking must not see a user's library, or the eval harness stops being reproducible. A lookup failure logs and returns unstamped results — the degradation rule applies.

The bridge reads the user's `(id, title, artist, status, track_number)` tuples per request and matches in Go. That is a full-table read per stamped request, which is cheap next to the provider fan-out it accompanies and is bounded by one user's library; the alternative (a stored normalized key column) needs a migration and a backfill whose normalization would have to match Go's exactly.

**Track-number fill.** `ports.TrackNumberFiller` + `catalogbridge.TrackNumberWriter` let the album-tracks endpoint write a missing `track_number` from the provider tracklist's own order. The client used to derive positions at view time and fire one `PATCH` per track from the phone. The write is fill-only server-side (so it is idempotent), runs on a detached context off the request path, and only fires for owned tracks that have no stored position.

**Blended slate.** `service/blend.go`'s `BuildBlendedSlate` groups the ranked results by kind, orders the sections by where each kind's strongest member ranks, drops the top result from its own section and caps each at ten. That is the Discover "All" view, and every one of those decisions is a ranking decision — it belongs next to the ranker and under the eval harness, not in a React component. It ships as `top_result` and `sections` alongside the flat `results`, which stay authoritative for paging and telemetry coordinates.

It takes two lists: the page the client will render and the full pre-truncation slate. Sections are grouped over the full slate so they arrive complete rather than filling in as the user pages, and each carries `has_more` so the client knows whether to offer "See all". The top result comes from the page, because `maybeExplore` shuffles the first page and the top card must match what is actually on screen. Both are computed on offset 0 only; later pages carry an empty slate, and the client reads them from page 1 like it already does for `search_id` and corrections.

**Artist content in one call.** `GET /v1/discovery/artists/{provider}/{externalId}/content` returns top tracks and albums together, fetched in parallel. The client was calling three per-provider endpoints (Deezer, SoundCloud, iTunes / Last.fm) and merging, deduping by a hand-rolled normalizer, back-filling artwork and sorting by release date on the device — a second, worse copy of what `GetArtistContentService` already does through the identity fan-out, MB anchor and `MergeReleases` core.

**Featured artists from text.** `domain.FeaturedFromText` parses "feat./ft./featuring/with" credits out of a title or subtitle and stamps them as `featured_artists` when no provider supplied structured credits. The client had the same regex.

**Enrichment `has_content`.** Each enrichment DTO now carries `has_content`, computed by `HasRenderableContent()` on the domain value. The client had its own per-provider predicate deciding whether a payload was worth rendering, which meant the client knew which fields Deezer returns. Deezer's predicate additionally counts featured artists, so a payload whose only content is guest credits no longer collapses to nothing.
