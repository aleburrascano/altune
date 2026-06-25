# Decorator — Structural

> GoF structural pattern. Source: https://refactoring.guru/design-patterns/decorator

**Intent.** Attach new behavior to an object by wrapping it in another object that implements the same interface and adds work before/after delegating.

## Problem
You need to add behaviors (logging, caching, retries, timing) to an object in varying combinations. Subclassing for every combination explodes; baking the behaviors into the base type makes it a god-object that can't be configured per call site.

## Solution
Wrap the object in a decorator that implements the *same* interface, does its extra work, then delegates to the wrapped instance. Because the interface is preserved, decorators stack recursively — `logging(retrying(client))` — and the client can't tell a decorated object from a bare one.

## In altune
**Go:** Idiomatic. A wrapper struct that holds a port and satisfies the *same* port, adding cross-cutting behavior — e.g. a `loggingProvider struct { inner ports.SearchProvider }` whose `Search` logs then calls `p.inner.Search`. Middleware in chi is the same idea for `http.Handler`. This is the "same interface + extra behavior" half of the family; the read-through caches (`EnrichmentCache`/`LyricsCache`) lean toward [[proxy]] because they *control the service's lifecycle/access*, not just decorate it — Decorator composition is client-chosen, Proxy manages the real object. Keep the distinction.
**RN/TS:** A wrapping component or a hook that composes another hook, adding behavior while preserving the contract (a `withErrorBoundary`-style wrapper, or `useLoggedQuery` wrapping `useQuery`). Composition over the same return shape.

Conceptual mapping — no single file is labelled "decorator", but the wrapper-implements-same-port shape is available throughout the ports in `services/go-api/internal/discovery/ports/`.

## When to reach for it
- Stackable, opt-in cross-cutting behavior (logging, metrics, retry) over a stable interface.
- You want call sites to choose the combination, transparently.

## When to skip it
- Only one decoration ever applies and it's not optional — fold it into the implementation.
- The wrapper changes the interface (then it's [[adapter]]) or manages the real object's existence/access (then it's [[proxy]]).
- A pass-through wrapper that adds nothing — deletion test: inline it (`../../refactoring/moving-features-between-objects.md` Remove Middle Man).

## Related
- Patterns: [[adapter]] (changes the interface; Decorator keeps it), [[proxy]] (same structure, but Proxy controls lifecycle/access; Decorator adds client-chosen behavior), [[composite]] (wraps many children vs Decorator's one)
- Refactoring moves: `../../refactoring/moving-features-between-objects.md` — Introduce Local Extension, Move Method
- Project rules: `../../backend/go-structs-interfaces.md` (optional behavior via type assertion), `../../backend/go-design-patterns.md`
