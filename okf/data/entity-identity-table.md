---
type: Database Table
title: entity_identity
description: The durable, cross-provider entity-identity map (the reverse identity bridge) keeping artwork/identity resolution correct even when MusicBrainz is absent from a search.
resource: services/go-api/migrations/008_discovery_entity_identity.sql, services/go-api/internal/discovery/adapters/persistence/entity_identity_repo.go
tags: [database-table, discovery, identity-resolution, artwork]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

`entity_identity` (`008_discovery_entity_identity.sql`): `provider TEXT NOT NULL`, `external_id TEXT NOT NULL`, `kind TEXT NOT NULL`, `mbid TEXT NOT NULL`, `xref JSONB NOT NULL DEFAULT '{}'::jsonb`, `resolved_at TIMESTAMPTZ NOT NULL DEFAULT now()`, with `PRIMARY KEY (provider, external_id, kind)`. `kind` is part of the key deliberately — the migration comment notes provider id spaces can numerically overlap across kinds (e.g. a Deezer artist id and album id could coincide), and every lookup already knows which kind it's resolving. A secondary index on `mbid` supports reverse lookups (all provider ids bridged to one MBID) for future backfill.

This table is keyed on stable provider ids — never fuzzy names — specifically so same-name entities (the migration's example: "Che") cannot inherit each other's identity. It is the durable, MusicBrainz-independent counterpart to the in-flight identity bridge computed during a live search's merge step (see [[identity-artwork]]): when MusicBrainz answers, the bridge it computes gets persisted here so a later, MB-absent search (rate-limited or circuit-open) still resolves the same entity's correct artwork identity-first.

`PgxIdentityStore` (`entity_identity_repo.go`) implements the `ports.IdentityStore` port (Postgres as source of truth; a Redis read-through fronts it in production per ubiquitous-language). `PersistBridges` upserts one row per `(provider, external_id)` in the `xref` map, all pointing at the same `mbid`, batched via `pgx.Batch` with `ON CONFLICT (provider, external_id, kind) DO UPDATE SET mbid = EXCLUDED.mbid, xref = EXCLUDED.xref, resolved_at = now()`; empty `mbid`/`xref` is a no-op. `LookupByProviderID` reads one row by `(provider, external_id, kind)`; both `pgx.ErrNoRows` and any other query error degrade to a plain miss (`ok=false`) — the comment is explicit that "a lookup failure must never break the search path."
