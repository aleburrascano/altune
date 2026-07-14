# Adapter — Structural

> GoF structural pattern. Source: https://refactoring.guru/design-patterns/adapter

**Intent.** Allow objects with incompatible interfaces to collaborate by wrapping one in a translator that exposes the interface the client expects.

## Problem
Your code speaks one interface; a third-party library or legacy service speaks another. You can't (or won't) edit the foreign code. Calling it directly would scatter conversion logic across every call site.

## Solution
Write an object that implements the interface your code expects and, internally, translates each call into the foreign service's vocabulary and back. The client depends only on the expected interface; the wrapped service stays untouched.

## In altune
**Go:** The architecture's backbone. Each external-API client under `internal/discovery/adapters/providers/` implements a consumer-defined `ports/` interface (`SearchProvider.Search`, `ArtistContentProvider.GetArtistTopTracks`), translating the provider's JSON shape into `domain.SearchResult`. This is the textbook Adapter: the provider's wire format is the "incompatible interface", the port is the "expected interface". The "wrap an unowned client + add methods" variant is the refactoring catalog's Introduce Local Extension (`deezerClient struct { *gen.Client }`).
**RN/TS:** Conceptual. A hook or thin wrapper that reshapes a third-party SDK's surface into the shape a feature's components consume (e.g. normalizing a library's response into the slice's `types.ts`). Composition, not a class adapter.

Verified: `services/go-api/internal/discovery/ports/ports_search.go:10` (the `SearchProvider` port that providers adapt to); providers in `services/go-api/internal/discovery/adapters/providers/*.go`.

**Adapter-the-GoF-pattern vs Adapter-the-hexagonal-layer.** Related but not identical. The hexagonal `adapters/` *layer* names anything implementing a port (repositories, caches, HTTP handlers). The GoF *pattern* is specifically the subset that translates a pre-existing, incompatible foreign interface into the port. A handler driving the app, or an in-memory fake, sits in the adapters layer but isn't GoF Adapter. Don't conflate the directory with the pattern.

## When to reach for it
- Integrating a third-party/legacy client whose interface you don't control.
- Pinning the consumer's contract (the port) so providers stay swappable behind it.

## When to skip it
- You own both sides — just change the interface directly; an adapter is pure indirection.
- The "translation" is a single field rename at one call site — inline it (deletion test: if removing the wrapper only touches that one site, it earns nothing).

## Related
- Patterns: [[bridge]] (designed upfront for two-axis variation, vs Adapter's retrofit), [[facade]] (new simplified interface over a *subsystem*, vs Adapter's translation of *one* interface), [[decorator]] (same interface + behavior, vs Adapter's *different* interface), [[proxy]] (same interface unchanged)
- Refactoring moves: `../../refactoring/moving-features-between-objects.md` — Introduce Local Extension, Introduce Foreign Method
- Project rules: `../../../.claude/rules/backend/go-structs-interfaces.md` (define interfaces where consumed), `../../../.claude/rules/backend/adapters-layer.md`
