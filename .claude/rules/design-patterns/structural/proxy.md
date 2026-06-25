# Proxy — Structural

> GoF structural pattern. Source: https://refactoring.guru/design-patterns/proxy

**Intent.** Provide a substitute that implements the *same* interface as a real object and controls access to it — adding lazy init, caching, access checks, or logging before delegating.

## Problem
You need to interpose behavior around a service object — defer its expensive creation, cache its results, gate access, log calls, manage its remote/lifecycle — without changing the service (often third-party) or making every client do it.

## Solution
Build a proxy that satisfies the same interface and holds (or lazily creates) the real service. The proxy runs its preliminary work, then delegates. Clients call the proxy exactly as they'd call the service — it's interchangeable.

## In altune
**Go:** Verified-instance shape. The read-through caches `EnrichmentCache` and `LyricsCache` are caching/protection proxies: they sit in front of the real enricher, serve from Redis when warm, and — critically — degrade to a **no-op when Redis is absent** (a Null Object variant of the proxy). Defined as ports in `internal/discovery/ports/`, satisfied by a Redis adapter or a no-op. Other Proxy flavors map cleanly to Go wrapper structs that satisfy a port: virtual (lazy `sync.Once` init), protection (auth gate), logging.
**RN/TS:** TanStack Query's cache *is* a caching proxy in front of the network — a hook reads cache-first, fetches on miss. You configure it rather than hand-roll it.

Verified (conceptual confirmation): `EnrichmentCache` / `LyricsCache` ports in `services/go-api/internal/discovery/ports/` (per the discovery context CLAUDE.md and ubiquitous-language: "Redis-backed in production, no-op when Redis is absent").

**Proxy vs Decorator.** Same wrapper structure; different intent. Proxy *controls access to / the lifecycle of* the real object and decides whether to even call it (cache hit ⇒ skip the service). Decorator *adds client-chosen behavior* and always delegates. The cache short-circuits the real enricher — that's Proxy.

## When to reach for it
- Caching, lazy init, access control, or logging around a service, behind its own interface.
- A graceful-degradation seam (no-op proxy when a dependency is missing) — the `go-safety` "useful zero value / Null Object" move.

## When to skip it
- No access/lifecycle concern — if you only add behavior and always delegate, it's [[decorator]]; if you change the interface, it's [[adapter]].
- A proxy that adds nothing measurable (a cache that never hits, a "lazy" init that's always needed) — deletion test, inline it.

## Related
- Patterns: [[decorator]] (adds behavior, always delegates; Proxy controls access/lifecycle), [[adapter]] (changes the interface; Proxy keeps it), [[facade]] (a *new* simpler interface; Proxy keeps the *same* one and is interchangeable)
- Refactoring moves: `../../refactoring/simplifying-conditional-expressions.md` — Introduce Null Object (the no-op cache); `../../refactoring/simplifying-method-calls.md` — Separate Query from Modifier (keep cache reads pure)
- Project rules: `../../backend/go-safety.md` (useful zero values, Null Object), `../../backend/go-structs-interfaces.md`, `../../backend/application-layer.md` (ports)
