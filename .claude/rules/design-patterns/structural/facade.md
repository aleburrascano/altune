# Facade — Structural

> GoF structural pattern. Source: https://refactoring.guru/design-patterns/facade

**Intent.** Provide one simplified interface over a complex subsystem, so clients depend on a small face instead of many moving parts.

## Problem
A subsystem (a set of providers, a library, a multi-step workflow) requires wiring many objects, ordering calls, and knowing internals. Clients that touch all of it become tightly coupled to its structure and break whenever the subsystem shifts.

## Solution
Introduce a facade that exposes only the operations clients actually need and orchestrates the subsystem behind it. Clients call one method; the facade fans out, sequences, and aggregates. The subsystem stays usable directly for the rare power case, but the common path is one call.

## In altune
**Go:** Verified-instance shape. The discovery service-layer use cases in `internal/discovery/service/*.go` are facades: `Service` presents a simple inward face (search, enrich) over scatter-gather across many providers, merge, rank, and reshape. The handler calls one use case; it never touches a provider directly. `internal/app/app.go` is the composition root that wires the subsystem the facade fronts. This aligns with the application-layer rule: use cases orchestrate; handlers and domain stay thin.
**RN/TS:** A feature hook is the facade — `useArtistDetail()` hides fetch + transform + cache + error handling, exposing a tidy `{ data, isLoading, error }` to the screen. The screen renders; the hook orchestrates the subsystem (TanStack Query, api-client, mappers).

Verified: `services/go-api/internal/discovery/service/search.go` (`Service` orchestrator — fanOut + mergeRankEnrich, per the context CLAUDE.md key-files map); composition root `services/go-api/internal/app/app.go`.

## When to reach for it
- A subsystem has grown complex and most clients need only a slice of it.
- You want a single, stable entry point that decouples callers from internal churn (the hexagonal service layer's whole job).

## When to skip it
- The "subsystem" is one object with a clean interface — a facade over it is a pass-through Middle Man (`../../refactoring/moving-features-between-objects.md` Remove Middle Man); let callers use it directly.
- *But* never collapse the service layer just because it's thin today — that hop is the hexagonal boundary (handler ↔ domain) and earns its place.

## Related
- Patterns: [[adapter]] (translates *one* existing interface; Facade defines a *new* simple one over *many*), [[proxy]] (same interface as its service, interchangeable; Facade is a new, narrower interface), [[mediator]] (centralizes bidirectional collaboration; Facade is a one-way simplifying front)
- Refactoring moves: `../../refactoring/moving-features-between-objects.md` — Hide Delegate (add the facade method), Remove Middle Man (when it's a pure pass-through)
- Project rules: `../../backend/application-layer.md`, `../../backend/go-dependency-injection.md`
