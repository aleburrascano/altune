# Chain of Responsibility — Behavioral

> GoF behavioral pattern. Source: https://refactoring.guru/design-patterns/chain-of-responsibility

**Intent.** Pass a request along a chain of handlers, each deciding to process it or forward it to the next.

## Problem
Sequential checks (auth, validation, rate-limit, logging) tangle into one growing conditional, tightly coupled and hard to reuse. You want to add, remove, or reorder steps without rewriting the block.

## Solution
Extract each check into an independent handler with a uniform "handle or pass on" contract; link them. Each handler does its slice of work and either short-circuits or delegates to the next. The chain can be reconfigured at runtime.

## In altune
**Go:** This is `net/http` / chi **middleware** — `func(http.Handler) http.Handler`. Auth, correlation-id (`httputil.CorrelationID`), structured-logging, and recovery middlewares each inspect the request and either short-circuit (write an error) or call `next.ServeHTTP`. Order is the chain. Not a class hierarchy — function composition. For non-HTTP chains, a slice of small handler funcs iterated until one returns "handled" is the idiomatic shape.
**RN/TS:** Rare. The closest is the `shared/api-client/` interceptor stack (auth → retry → error-mapping), each interceptor transforming or short-circuiting the request/response. Don't hand-roll a node-linked chain in component code.
<Conceptual — middleware verified as the canonical Go shape; no class-based CoR exists.>

## When to reach for it
- A request must pass an ordered, runtime-configurable set of independent processing steps.
- HTTP cross-cutting concerns (the default answer in this codebase).

## When to skip it
A fixed, short sequence of steps is just straight-line code — don't build a chain for two guard clauses. Rule of Three: one ad-hoc `if`-ladder beats a handler abstraction until the steps multiply and need reordering.

## Related
- Patterns: [[command]], [[mediator]], [[observer]] (alternatives for wiring sender→receiver)
- Refactoring moves: `../../refactoring/simplifying-conditional-expressions.md` (Replace Nested Conditional with Guard Clauses — each handler is one guard lifted out)
- Project rules: `../../backend/go-design-patterns.md`
