---
type: Provider Integration
title: MusicBrainz Adapter
description: MusicBrainz is the identity hub — it mints MBIDs that unlock HD Cover Art Archive/Fanart.tv artwork and bridges every result to its Deezer/Spotify/Discogs/Last.fm/Wikidata/Apple ids.
resource: services/go-api/internal/discovery/adapters/providers/musicbrainz.go, services/go-api/internal/discovery/adapters/providers/musicbrainz_featured.go, services/go-api/internal/discovery/adapters/providers/musicbrainz_enrichment.go
tags: [discovery, provider, musicbrainz, identity, artwork]
verified_commit: e238cc3671d1719837686c667242c7d88fc376d2
---

`MusicBrainzAdapter` (`musicbrainz.go`) hits `musicbrainz.org/ws/2` keylessly, gated only by a required User-Agent (`cfg.MusicBrainzUserAgent`) — a missing UA returns 403. `rateLimit(ctx)` enforces the hard 1 req/sec ceiling via a mutex that reserves a future slot per caller (fixing an earlier bug where concurrent callers could stamp the same baseline and burst together, triggering MB 503s). `Search`/`SearchStructured` cover artist/recording/release-group (`searchKind`, `mbEntity`); recording results additionally carry a `featured_artists` extra parsed from multi-artist credits (`extractMBFeatured` over the joinphrase-aware artist-credit list). `ResolveArtistIdentity` resolves a name to an `ArtistIdentity` (MBID, disambiguation, birth year, area, type); `ValidateArtistAlbums`/`LookupAlbumArtist` feed the consensus/contamination-check engine (see [merge-dedup](../backend/discovery/merge-dedup.md)).

The code also adds an `mbMemo` TTL cache (`mbMemoTTL = 6h`, keyed by normalized name / MBID) around identity resolution and release-group fetches — a performance optimization cutting repeated MB round-trips under the 1 req/s ceiling — not mentioned in `docs/providers/musicbrainz.md`. `ListArtistDiscography` (a discography-listing counterpart to `ValidateArtistAlbums`) likewise exists in code without appearing in the doc's capability list.

`musicbrainz_enrichment.go` implements `ports.MetadataEnricher`: `Lookup` fetches `inc=genres+ratings+url-rels` (artist) or `inc=genres+ratings` (release-group), yielding curated genres (`sortedGenres` — vote-count descending, alphabetical ties), `rating`, and (artist only) `externalIDsFromRelations` — parsing url-relations into `external_ids{discogs,wikidata,spotify,deezer,itunes}` for the cross-provider identity bridge (ADR-0011, see [identity-artwork](../backend/discovery/identity-artwork.md)). `ResolveMBID` does a strict normalized-match lookup (title + artist-credit), never fuzzy — no match returns `""` so the service treats it as "nothing to enrich."

Both search and enrichment are config-gated by `cfg.HasMusicBrainz()`. Cover Art Archive (a sibling adapter, `coverartarchive.go`) runs first in the artwork chain, consuming the release-group MBIDs this adapter supplies (see [artwork-chain](artwork-chain.md)). No popularity signal — MB `rating` is sparse/community-driven; Deezer stays the ranking popularity primary.
