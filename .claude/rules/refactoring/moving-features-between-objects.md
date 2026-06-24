---
description: "Refactoring catalog — Moving Features Between Objects (Fowler, adapted to altune Go + RN/TS). On-demand reference for /tighten-backend, /tighten-frontend, /improve-codebase-architecture."
source: https://refactoring.guru/refactoring/techniques/moving-features-between-objects
---

# Refactoring — Moving Features Between Objects

A feature lives in the wrong module — wrong locality. The moves here relocate a method, field, or cluster of responsibility to where its data and callers actually are, so each seam wraps what it owns.

## When to reach for these
After a review skill flags feature envy (a function reaching across a boundary for another module's data), a god-module doing two jobs, or a chain of pass-through hops. Name the technique below, then move. These attack low locality and shallow pass-through layers — the opposite of a deep module.

## Techniques

### Move Method
- **Smell** — a function leans on another module's data/exports more than its own — feature envy.
- **Move** — relocate it to the module whose data it uses; leave a thin forwarder only if callers can't migrate yet.
- **altune** — Go: a service func that mostly reads one aggregate's fields belongs as a method on that domain type (receiver), or in that type's package. RN-TS: a helper in `featureA` that only manipulates `featureB`'s state should live in `featureB`'s hooks — not cross-imported.
- **Skip when** — the function legitimately orchestrates several modules (a use case); that's the service layer's job, not envy.

### Move Field
- **Smell** — a struct field / state value is read and written mostly by another module.
- **Move** — relocate the field to the type that owns its invariants; update references.
- **altune** — Go: move the field to the aggregate that enforces its rule (e.g. `audio_ref` lives on `Track` because the `audio_ref ↔ AcquisitionStatus` invariant is Track's). RN-TS: lift a `useState` into the store/hook that actually drives it, instead of prop-drilling writes back up.
- **Skip when** — the field is a genuine cross-cutting value object (e.g. `UserId` in `shared/`) — keep it shared.

### Extract Class
- **Smell** — one module does two jobs; its interface mixes unrelated vocabularies — low cohesion, wide blast radius.
- **Move** — split the second responsibility into its own type/file with its own fields and methods.
- **altune** — verified instance: discovery's per-provider detail enrichment was split into cohesive files — `services/go-api/internal/discovery/domain/{deezer,discogs,lastfm}_enrichment.go` — each a distinct value object, not one fat `Enrichment`. RN-TS: pull a tangled screen's data logic into a custom hook (the slice's `hooks/`), leaving `ui/` presentational.
- **Skip when** — extraction to `shared/` with only one consumer — YAGNI; needs 2+ real consumers. Splitting within a slice/context is fine.

### Inline Class
- **Smell** — a type/file does almost nothing and earns no future responsibility — a shallow pass-through.
- **Move** — fold its members back into its sole caller and delete it. The deletion test in reverse.
- **altune** — Go: a one-method "manager" wrapping a single repo call — inline it into the service. RN-TS: a wrapper component that only forwards props, or a hook that just re-exports another — inline and delete.
- **Skip when** — the thin type is a port (interface) deliberately defining a seam for testability/adapters — keep it; that depth is intentional.

### Hide Delegate
- **Smell** — callers reach `a.getB().doThing()` — they know A's internal structure; B leaks through A's interface.
- **Move** — add a delegating method on A so callers ask A directly; B stays hidden.
- **altune** — Go: a handler walking `svc.Repo().Find()` should call `svc.Find()`; the repo stays an internal detail of the service. RN-TS: expose `useLibrary().addTrack()` rather than handing callers the raw store to mutate.
- **Skip when** — A would become a pass-through god-object forwarding dozens of B's methods — that's the next smell.

### Remove Middle Man
- **Smell** — a module is mostly delegating methods that add nothing — a shallow forwarder layer.
- **Move** — let callers talk to the real target directly; delete the dead-weight forwarders. Inverse of Hide Delegate.
- **altune** — Go: if a service method is a one-line `return s.repo.X(...)` with no logic and the handler could hold the port, drop the hop. RN-TS: a "facade" hook that only re-calls another hook's methods 1:1 — remove it.
- **Skip when** — the middle layer is the hexagonal boundary (service mediating handler↔domain) — it earns its place even when thin today; don't collapse the architecture.

### Introduce Foreign Method
- **Smell** — you need a helper on a type you don't own (stdlib, generated client) and can't edit it.
- **Move** — write a free function taking that type as its first parameter; keep it beside the caller.
- **altune** — Go: a package-level `func normalizeISRC(s string) string` next to the discovery code, not a fork of stdlib. RN-TS: a pure util in the slice's local module taking the foreign object as an arg.
- **Skip when** — the helper is reused by 2+ features — promote to `shared/lib/` instead of duplicating the foreign method.

### Introduce Local Extension
- **Smell** — you need several methods on an unowned type; scattering foreign functions gets noisy.
- **Move** — wrap or compose the type into a local one carrying the extra methods. Go has no subclassing — use struct embedding or a wrapper struct satisfying an interface; RN-TS uses composition/HOC or a wrapping hook.
- **altune** — Go: embed the generated provider client in a local `type deezerClient struct { *gen.Client }` and add methods there — composition over inheritance. RN-TS: wrap a third-party component in a local one that adds the behavior your slice needs.
- **Skip when** — one or two helpers suffice — use Introduce Foreign Method; a whole extension type is overkill (depth without leverage).
