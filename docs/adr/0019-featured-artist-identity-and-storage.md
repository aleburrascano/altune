# ADR-0019: Featured-artist identity and storage

- **Status:** Accepted
- **Date:** 2026-07-05
- **Deciders:** solo + Claude
- **Context tags:** [pattern, ddd, persistence]

> **Vault note:** the `software-architecture-design` vault MCP was unavailable at design time. The DDD modeling below is `[INFERRED]` from standard building-block guidance (Value Object vs Entity, Repository per Aggregate) and should be re-checked against the vault when it is reachable.

## Context

The `featured-artists` spec (`docs/specs/featured-artists/spec.md`) sources structured featured-artist data from the discovery pipeline (MusicBrainz `artist-credit` + Deezer `contributors`) and persists it on saved tracks, tappable and browsable ("everything featuring X"). This forces two modeling decisions: **what a featured artist *is*** in the domain, and **how it is stored** so that queries by artist are clean.

A featured artist carries a display name and, when the provider supplies it, a canonical id (MusicBrainz MBID and/or Deezer artist id). The browse feature needs a stable grouping key so that the same artist across many tracks collapses to one row.

The discovery context already has an `entity_identity` table (`services/go-api/migrations/008_discovery_entity_identity.sql`) mapping `(provider, external_id, kind) → (mbid, xref)`. It is tempting to reuse it as the featured-artist identity store.

## Decision

**1. `FeaturedArtist` is a value object, not an entity.** It is immutable, compared by attributes, and has no independent lifecycle — it exists only as a credit on a `Track`. It carries `name`, optional `mbid`, optional `deezerId`, and `role` (only `"featured"` populated in v1). This follows "wrap domain primitives / model credits as value objects" rather than promoting a full `Artist` aggregate (YAGNI — no write-side artist surface exists yet).

**2. Storage is a normalized join, keyed on a computed identity key.** Two tables:
- `featured_artists` — one canonical row per `(user_id, identity_key)`, where `identity_key = COALESCE(mbid, 'dz:'||deezer_id, 'name:'||norm_name)`. MBID wins, Deezer id next, normalized name last.
- `track_featured_artists` — ordered membership (`track_id ↔ featured_artist_id`, `position`).

This makes "everything featuring X" and per-artist counts index-clean joins, and dedups identity across the whole library — the shape JSONB-on-`tracks` fights.

**3. Featured-artist identity stays SEPARATE from discovery's `entity_identity`.** `entity_identity` is a discovery-context concern (cross-provider search identity, per its bounded context). Featured artists are a **catalog** concern, per-user, tied to the `tracks` aggregate. Coupling catalog persistence to a discovery table would violate the dependency rule. The featured-artist grouping key is computed independently in catalog; discovery's identity bridge may *inform* the resolver's merge (it runs inside discovery) but does not own catalog's storage.

## Alternatives considered

- **JSONB array column on `tracks`** (`featured_artists jsonb`). Simplest, zero joins, trivial new saves. Rejected: "everything featuring X" becomes a containment query with awkward grouping/counting — it fights exactly where the browse feature pulls hardest.
- **Full `Artist` aggregate.** Promote featured artists to first-class entities with their own repository. Rejected as premature — no write-side artist behavior (subscribe, merge, edit) exists; a value object keyed on id delivers grouping + tappable-by-id without the aggregate machinery. Revisit when an artist write-surface lands.
- **Reuse `entity_identity` for featured-artist identity.** Rejected — crosses the bounded-context boundary (catalog depending on a discovery table) and conflates search identity with library membership.

## Consequences

- New migration `010_track_featured_artists.sql`; `Track` gains an ordered `FeaturedArtists []FeaturedArtist` collection; the catalog repo writes the join in the add-track transaction.
- The resolver (discovery) and the store (catalog) are wired through a port + bridge adapter, never a direct import — mirroring `playback/catalogbridge`.
- If MBID coverage is poor for some catalog, name-keyed rows may fragment on spelling variants; acceptable for v1 and improvable by a later identity pass.
