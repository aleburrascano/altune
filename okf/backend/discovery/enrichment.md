---
type: Subsystem
title: Discovery enrichment
description: Detail-screen enrichment services that fetch MusicBrainz, Discogs, Last.fm, and Deezer metadata (including lyrics) on-demand when a user opens an entity, never on the ranking path.
resource: services/go-api/internal/discovery/service/enrich/, services/go-api/internal/discovery/service/enrichment.go, services/go-api/internal/discovery/service/enrich.go, services/go-api/internal/discovery/domain/enrichment.go, services/go-api/internal/discovery/domain/deezer_enrichment.go, services/go-api/internal/discovery/domain/deezer_lyrics.go, services/go-api/internal/discovery/domain/discogs_enrichment.go, services/go-api/internal/discovery/domain/lastfm_enrichment.go, services/go-api/internal/discovery/service/cached_lookup.go
tags: [discovery, enrichment, musicbrainz, discogs, lastfm, deezer, lyrics, cache, subsystem]
verified_commit: c324e0716c50cc6d5e3d7a5255ac9f7552bc0df1
---

Detail-open (not search-time) enrichment lives behind five parallel use-case services, one per provider surface, all following the same shape: resolve an id from a name, look up the provider's rich payload, cache it, degrade to an empty value + nil error on any failure (never surface an error to the detail endpoint).

`EnrichmentService` (`service/enrichment.go`) is the MusicBrainz path: resolves an MBID via strict name match (or accepts one already known from search), looks up `domain.MBEnrichment` (curated genres, year, rating, release types, `ExternalIDs` cross-provider bridge, HD `ArtworkURL` via the `ports.TaggingArtworkResolver` chain), and negative-caches an unresolved name via `ports.EnrichmentCache`. `WithMBIDMemo` wires a `ports.MBIDIndex` so a resolved MBID is remembered for the search path's artwork tier (see [identity-artwork](identity-artwork.md)).

The four sibling services in `service/enrich/` — `DeezerEnrichmentService`, `DiscogsEnrichmentService` + `DiscogsArtistEnrichmentService`, `LastFmEnrichmentService`, `LyricsService` — all route their resolve→lookup→cache dance through the shared generic helper `service.CachedLookup[T]` (`service/cached_lookup.go`), which encodes one rule: positive hit → return; negative hit → empty; transient error → not cached (retry later); definitive miss → negative-cached. Each maps the wire `(kind, title, subtitle)` to the provider's native name shape (e.g. Last.fm's `lastfmLookupNames` swaps title/subtitle depending on kind).

Domain value objects are immutable, non-persisted read surfaces with an `IsZero()`/`Empty*()` pair each: `domain.MBEnrichment`, `domain.DeezerEnrichment` (BPM/gain/label/UPC, plus a `Featured []FeaturedArtist` list of guest contributors from the same detail call — consumed by the "Featuring" row rather than the Deezer detail section, so deliberately excluded from `IsZero()`), `domain.DeezerLyrics` (plain + LRC-style `SyncedLyricLine`s — the one axis no other provider carries), `domain.DiscogsEnrichment`/`DiscogsArtistEnrichment` (credits/styles/community), `domain.LastFmEnrichment` (listen-based popularity, tags, similar artists). All empty constructors return non-nil slices/maps so the wire mapping never emits `null`.
