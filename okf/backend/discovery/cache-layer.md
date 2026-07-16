---
type: Subsystem
title: Discovery cache layer
description: The Redis-backed read-through caches for MB enrichment, artwork, and final ranked results — each a no-op when Redis is absent, and each a distinct GoF Proxy keyed and TTL'd for its own consistency need.
resource: services/go-api/internal/discovery/adapters/cache/enrichment_cache.go, services/go-api/internal/discovery/adapters/cache/artwork_cache.go, services/go-api/internal/discovery/adapters/cache/result_cache.go
tags: [discovery, cache, redis, proxy-pattern, subsystem]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

Three caches, three consistency shapes, one house rule: a nil Redis client degrades every method to a no-op (Get miss / Set no-op) rather than an error, so the service runs correctly, just uncached, when Redis is absent — the caching/protection Proxy pattern with a Null Object fallback (`.claude/rules/design-patterns/structural/proxy.md`). Note: the underlying `internal/shared/redis` client itself is only `nil` when misconfigured (empty/invalid URL); a *ping* failure still returns a non-nil, unreachable client, so callers here must tolerate failing Redis calls, not only nil-check (see [shared-infra](../shared-infra.md)).

`RedisEnrichmentCache` (`enrichment_cache.go`) is the MB enrichment cache and doubles as two other ports off the same data: `ports.IdentityBridge.ExternalIDs` reads the cross-provider ids out of an already-cached `(kind, mbid)` positive entry (no extra MB round-trip), and `ports.MBIDIndex` (`LookupMBID`/`RememberMBID`) is a cache-only name→MBID memo the search path reads to attach an MBID to a non-MB result. Positive TTL 14 days, negative (unresolved name) 24h.

`RedisArtworkCache` (`artwork_cache.go`) keys on `(kind, title, subtitle, mbid)` and stores `{url, source, confidence}`. Its `Set` has an overwrite guard: a lower-`ArtworkConfidence` write is refused if an existing entry already has a higher one — a name-search result can never clobber a proven-identity image (see [identity-artwork](identity-artwork.md)). TTLs are confidence- and kind-differentiated: identity images 14 days, provisional name-resolved images 48h (so they can upgrade), and negative (no-image) TTLs staggered per kind — tracks 6h, albums 12h, artists 24h — because tracks gain artwork soonest and artists least.

`RedisResultCache` (`result_cache.go`) is different in kind: an **app-wide, not per-user**, 45-second cache of a query's final ranked list (`ports.ResultCache`, `ports/ports_result_cache.go`). Discovery results are catalog-derived, not user-specific, so a shared key is correct — and the short window is the point: it smooths provider-drop-out and cache-warmth variance so an identical query returns an identical list across a short burst, without masking a shipped ranking change or a newly-acquired track for more than a minute.

A fourth generic, `RedisNameKeyedCache[T]` (`name_keyed_cache.go`), backs the sibling detail-enrichment caches (Deezer/Last.fm/Discogs/lyrics) via `ports.NameKeyedCache[T]` — see [enrichment](enrichment.md).
