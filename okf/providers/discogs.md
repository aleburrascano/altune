---
type: Provider Integration
title: Discogs Adapter
description: Discogs supplies the deepest structured metadata — per-track/release credits, styles, label/catalog data, community demand signal, and artist biography/links — plus a ≤600px artist-image artwork fallback.
resource: services/go-api/internal/discovery/adapters/providers/discogs.go, services/go-api/internal/discovery/adapters/providers/discogs_enrichment.go
tags: [discovery, provider, discogs, credits, artwork]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

`DiscogsAdapter` (`discogs.go`) authenticates every call with `Authorization: Discogs token=<DISCOGS_TOKEN>` plus a descriptive User-Agent (`doGet`), gated by `cfg.HasDiscogs()`. A local `rateLimit()` mutex enforces ~1 req/sec, well under Discogs's documented 60 req/min (verified via `X-Discogs-RateLimit` headers). `Resolve` implements `ArtworkResolver` for artist images only, wired last in the artwork chain (≤600px — below Cover Art Archive's 1200px). `ResolveDiscogsArtist` + `FetchArtistReleases` feed the identity/consensus engine via album-overlap disambiguation when a name search returns 2+ candidates (see [merge-dedup](../backend/discovery/merge-dedup.md)).

The adapter also exposes `ResolveByIdentity` (`discogs.go`), which fetches an artist's primary image directly from a bridged Discogs id (`ports.ArtworkIdentity`) rather than a name search — an identity-first artwork path not documented in `docs/providers/discogs.md` (which only describes the name-search `Resolve`), added by the more recent identity-artwork-resolution work (see [identity-artwork](../backend/discovery/identity-artwork.md) and [artwork-chain](artwork-chain.md)).

`discogs_enrichment.go` implements `ports.DiscogsEnricher`: `ResolveMasterID` resolves `(artist, album)` to a Discogs master id via the structured `artist=&release_title=&type=master` search (intentionally fuzzy — a deluxe/reissue master can win over the original, accepted since it's display-only); `LookupAlbum` fetches the master (genres, styles, year, per-track `extraartists`) and its main release (labels, formats, companies, `community{have,want,rating}`), preferring release-level credits over the per-track fallback, capped at `discogsCreditsCap = 60`. `ResolveArtistID` + `LookupArtist` supply artist bio (BBCode-stripped via `cleanDiscogsProfile`), real name, aliases, name variations, group/member links, and categorized external links (`mapLinks`, capped at `discogsLinksCap = 10`).

Both enrichment surfaces are display-only, off the ranking path, cached via `RedisDiscogsEnrichmentCache`/`RedisDiscogsArtistEnrichmentCache` (30d positive / 24h negative, see [enrichment](../backend/discovery/enrichment.md)), config-gated by `cfg.HasDiscogs()`. No ISRC/MBID — Discogs owns its own integer id space, so all matching (master resolution, artist resolution) stays fuzzy and name-based.
