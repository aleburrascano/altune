---
type: Subsystem
title: Discovery identity & artwork resolution
description: The durable EntityIdentity bridge and identity-first artwork chain that keep cross-provider merge and artwork correct for same-name entities even when MusicBrainz is absent from a given search.
resource: services/go-api/internal/discovery/domain/identity.go, services/go-api/internal/discovery/adapters/persistence/entity_identity_repo.go, services/go-api/internal/discovery/adapters/cache/identity_store_cache.go, services/go-api/internal/discovery/adapters/cache/artwork_cache.go, services/go-api/internal/discovery/adapters/providers/artwork_chain.go, services/go-api/internal/discovery/ports/ports_artwork.go
tags: [discovery, identity, artwork, musicbrainz, cache, persistence, subsystem]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

The load-bearing principle (per `ARCHITECTURE.md`): everything merges by identity, never by bare name, because same-name entities (the "Che" problem) otherwise collide. Two structures carry this in-request and durably.

`domain.ArtistIdentityProfile` (`domain/identity.go`) is an in-flight read-model — MBID, Discogs id, genre cluster, ISRC registrants, MB-confirmed titles — assembled per search from provider signals; `AlbumVerdict` (Confirmed/Contamination/Suspect/Unknown) is the consensus classification an album gets against it (see [merge-dedup](merge-dedup.md)).

The durable counterpart is `entity_identity` (see [entity-identity-table](../../data/entity-identity-table.md)): `ports.IdentityStore` maps `(provider, external_id, kind) → (mbid, xref)`. `PgxIdentityStore` (`adapters/persistence/entity_identity_repo.go`) is the source of truth — `PersistBridges` upserts one row per bridged provider id when MusicBrainz answers a search; `LookupByProviderID` reads it back. `RedisIdentityStore` (`adapters/cache/identity_store_cache.go`) fronts it with a 30-day read/write-through cache, degrading transparently to the inner store when Redis is nil or on a miss — this is what lets a later MB-absent search still resolve a provider-only result's identity deterministically instead of guessing by name (`service/enrich.go`'s `enrichOne`, tagged `"durable-identity"` in `artwork_path`).

Artwork resolution consumes this identity. `ChainedArtworkResolver` (`adapters/providers/artwork_chain.go`) tries identity-only resolvers first via `ResolveWithIdentityTagged` (Discogs-by-id, CAA/Fanart by MBID) — these never name-search — and only falls to name-search resolvers (`ResolveTagged`) when no identity source has the image. `ports.ArtworkConfidence` (`ArtworkConfidenceIdentity` > `ArtworkConfidenceName` > `None`) grades the result so `RedisArtworkCache` (`adapters/cache/artwork_cache.go`) can refuse to let a weaker name-guess overwrite a proven-identity image, and gives identity hits a 14-day TTL vs. 48h for provisional name hits and a per-kind negative TTL (tracks churn fastest, artists slowest). See [artwork-chain](../../providers/artwork-chain.md) for the ordered provider chain itself.
