# ADR-0013: Keep five distinct detail enrichers; no generic enricher registry

- **Status:** Accepted
- **Date:** 2026-06-22
- **Deciders:** solo + Claude
- **Context tags:** [pattern | layer]

## Context

The discovery context exposes five detail-enrichment surfaces (MusicBrainz, Discogs, Last.fm, Deezer, Lyrics), each as its own service (`*EnrichmentService`/`LyricsService`), wired through a `DetailEnrichers` struct and a per-provider handler endpoint. A `/tighten-backend` review flagged the five parallel services + the bundling struct as stamp/communicational coupling, and asked whether they should collapse behind one `DetailEnricher` interface + a kind→enricher registry dispatched from a single endpoint.

The duplication that motivated the question is, on inspection, already resolved at the layers where it mattered: the cache adapters collapsed to one generic `RedisNameKeyedCache[T]`, and the service-layer resolve→lookup→cache dance is factored into the shared `CachedLookup` helper (4 consumers). What remains distinct between enrichers is real, not incidental: different query params (MB keys on kind+mbid; Discogs on artist+album; Last.fm on kind+title), different DTO shapes, and different cache key strategies (stable MBID vs fuzzy name).

## Decision

Keep the five enrichers as distinct services with distinct handler endpoints. Do **not** introduce a `DetailEnricher` interface, a registry, or a single type-erased dispatch endpoint. The handlers stay honest shallow shells (parse → call service → map DTO → write); the shared transport and cache plumbing remain the only things factored out.

## Alternatives considered

| Alternative | Why not |
|---|---|
| `DetailEnricher` interface + kind→enricher registry, one generic endpoint | Trades five readable, isolated, statically-typed handlers for parametric param-parsing and a type-erased return. The per-enricher variation (params, DTOs, cache keys) is intentional; a registry hides it behind a weaker abstraction. [vault: wiki/concepts/DRY Principle.md] — duplication of *shape* is cheaper than the wrong abstraction over genuinely different behaviour. |
| Unify the MBID-keyed and name-keyed caches behind one typed cache port | The key-type difference reflects a domain difference (MBID is stable, names are fuzzy/autocorrected). One real MBID-keyed consumer does not justify a generic (rule of three). |

## Consequences

### What becomes easier
- Each enricher reads top-to-bottom with its own typed params and DTO; bugs stay local.
- Adding a provider with a genuinely new shape doesn't have to fit a pre-committed interface.

### What becomes harder
- Adding the Nth enricher is still N edits across a few files (service ctor, `DetailEnrichers` field, handler endpoint, DTO mapper). Accepted: this is linear, mechanical, and visible.

### What we're committing to (and the cost to reverse)
- If the roster grows past a handful of enrichers that converge on one shape (same params, same DTO contract), revisit: that is the point where a registry stops being premature. Reversing this ADR is cheap — it's an additive refactor, not a data or API change.

## Vault references

- [vault: wiki/concepts/DRY Principle.md]
- [vault: wiki/concepts/YAGNI Principle.md]
- [vault: wiki/concepts/Coupling and Cohesion.md]

## Related

- Predecessor: ADR-0011 (identity-based merge), ADR-0007 (unified music search)
- Surfaced by: `/tighten-backend` review, 2026-06-22 (finding G1)
