---
type: Provider Integration
title: Discogs Adapter
description: Discogs contributes album consensus for artist discographies, identity resolution, and a ≤600px artist-image artwork fallback. Its credits/bio enrichment surface was retired 2026-07-24.
resource: services/go-api/internal/discovery/adapters/providers/discogs.go
tags: [discovery, provider, discogs, credits, artwork]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

`DiscogsAdapter` (`discogs.go`) authenticates every call with `Authorization: Discogs token=<DISCOGS_TOKEN>` plus a descriptive User-Agent (`doGet`), gated by `cfg.HasDiscogs()`. A local `rateLimit()` mutex enforces ~1 req/sec, well under Discogs's documented 60 req/min (verified via `X-Discogs-RateLimit` headers). `Resolve` implements `ArtworkResolver` for artist images only, wired last in the artwork chain (≤600px — below Cover Art Archive's 1200px). `ResolveDiscogsArtist` + `FetchArtistReleases` feed the identity/consensus engine via album-overlap disambiguation when a name search returns 2+ candidates (see [merge-dedup](../backend/discovery/merge-dedup.md)).

The adapter also exposes `ResolveByIdentity` (`discogs.go`), which fetches an artist's primary image directly from a bridged Discogs id (`ports.ArtworkIdentity`) rather than a name search — an identity-first artwork path not documented in `docs/providers/discogs.md` (which only describes the name-search `Resolve`), added by the more recent identity-artwork-resolution work (see [identity-artwork](../backend/discovery/identity-artwork.md) and [artwork-chain](artwork-chain.md)).

`discogs_enrichment.go` implements `ports.DiscogsEnricher`: `ResolveMasterID` resolves `(artist, album)` to a Discogs master id via the structured `artist=&release_title=&type=master` search (intentionally fuzzy — a deluxe/reissue master can win over the original, accepted since it's display-only); `LookupAlbum` fetches the master (genres, styles, year, per-track `extraartists`) and its main release (labels, formats, companies, `community{have,want,rating}`), preferring release-level credits over the per-track fallback, capped at `discogsCreditsCap = 60`. `ResolveArtistID` + `LookupArtist` supply artist bio (BBCode-stripped via `cleanDiscogsProfile`), real name, aliases, name variations, group/member links, and categorized external links (`mapLinks`, capped at `discogsLinksCap = 10`).

Both enrichment surfaces are display-only, off the ranking path, cached via `RedisDiscogsEnrichmentCache`/`RedisDiscogsArtistEnrichmentCache` (30d positive / 24h negative, see [enrichment](../backend/discovery/enrichment.md)), config-gated by `cfg.HasDiscogs()`. No ISRC/MBID — Discogs owns its own integer id space, so all matching (master resolution, artist resolution) stays fuzzy and name-based.

## Retired: the credits/bio enrichment surface (2026-07-24)

`discogs_enrichment.go` and everything above it is **gone**: `ResolveMasterID` / `LookupAlbum` (credits, styles, label/catalog, companies, community signal) and `ResolveArtistID` / `LookupArtist` (profile, aliases, members, groups, links), the `DiscogsEnricher` port, both enrichment services, both Redis name-keyed caches, the `domain.Discogs*` value objects, and the two `GET /v1/discovery/enrichment/discogs[/artist]` routes.

Why: the client surface that rendered them was deleted in the 2026-07-16 detail structure audit ("the retired enrichment surface is deleted, not dark"), and nothing replaced it. The server kept resolving, caching and serving data no consumer read — cost and maintenance for output nobody consumed. Removed rather than left dark so the dead weight is not mistaken for a live integration.

**What Discogs still does, unchanged** — it is one of the more load-bearing providers:

- **Album consensus** — `ResolveDiscogsArtist` + `FetchArtistReleases` feed the artist-discography consensus set (`internal/app/search_wiring.go`), so Discogs still shapes which releases an artist is credited with.
- **Artwork** — `Resolve` / `ResolveByIdentity` / `ArtworkSource` sit in the artwork chain as the ≤600px artist-image fallback.
- **Identity** — `ResolveByIdentity` resolves by an MB-asserted Discogs id, so it participates in the cross-provider identity bridge.

If a credits or liner-notes surface is ever wanted in the app again, this is a UI question first: the provider is still connected and the lookup code is recoverable from git history.

Rationale for this adapter lives in `services/go-api/internal/discovery/CLAUDE.md`; the Go files carry no comments.
