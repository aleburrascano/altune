---
description: "Refactoring catalog — Simplifying Conditional Expressions (Fowler, adapted to altune Go + RN/TS). On-demand reference for /tighten-backend, /tighten-frontend, /improve-codebase-architecture."
source: https://refactoring.guru/refactoring/techniques/simplifying-conditional-expressions
---

# Refactoring — Simplifying Conditional Expressions

Conditional logic that has outgrown its locality — nested `if`s, control flags, duplicated branch fragments, type switches doing a strategy's job. These moves trade tangled branching for flat, named, deep predicates and dispatch seams.

## When to reach for these
After a review skill flags a deep `if/else` nest, a wall of `||`, a boolean threaded through a loop, or a `switch` on a kind/type that keeps growing — name the specific technique below and apply its move. This group is the primary answer to "cyclomatic complexity here is high" and "I can't tell the happy path from the edge cases."

## Techniques

### Decompose Conditional
- **Smell** — a fat `if (complex test) { complex then } else { complex else }` where each part is itself a paragraph.
- **Move** — extract the test, the then-branch, and the else-branch into named functions; the conditional becomes three readable calls.
- **altune** — Go: pull the predicate into a named bool or `func` (`isStaleEnrichment(e)`); pull each branch into a helper. RN-TS: hoist the test into a named `const canSubmit = …` or a `useMemo`, branch bodies into local functions or sub-components. Names ARE the documentation — the interface deepens.
- **Skip when** — the branches are one-liners; naming them adds depth without payoff.

### Consolidate Conditional Expression
- **Smell** — several separate checks that all return/throw the same thing.
- **Move** — OR/AND them into one expression behind a single named predicate.
- **altune** — Go's `go-code-style` rule already mandates this: 3+ operands → extract a named bool (`isPublicVerified := …`). The consolidated predicate becomes a candidate for its own tested function. RN-TS: collapse stacked early-returns that yield the same fallback into one guarded `const`.
- **Skip when** — the checks short-circuit expensive work and order matters, or they genuinely lead to *different* results.

### Consolidate Duplicate Conditional Fragments
- **Smell** — the same statement sits at the top (or bottom) of every branch.
- **Move** — hoist the identical fragment out of the conditional.
- **altune** — Go: a `defer cancel()` or a log line repeated in each branch lifts above the `if`. RN-TS: identical `setLoading(false)` in every branch → one line after the conditional, or a `finally`. Watch the `append`/slice-aliasing trap (`go-safety`) when hoisting mutations.
- **Skip when** — the fragments only look identical; if one branch needs it *before* a side effect and another *after*, leave them.

### Remove Control Flag
- **Smell** — a `found := false` / `done := false` boolean steering loop continuation.
- **Move** — replace it with `return`, `break`, or `continue`.
- **altune** — Go: extract the loop into a function and `return` the moment you have the answer — kills the flag and flattens nesting. RN-TS: `Array.prototype.find`/`some`/`every` replace a hand-rolled flag loop outright.
- **Skip when** — the flag also carries a *value* you need after the loop (then it's an accumulator, not a control flag).

### Replace Nested Conditional with Guard Clauses
- **Smell** — pyramid of nested `if`s where the real work is buried at the deepest indent and the happy path is invisible.
- **Move** — turn each edge case into an early-return guard at the top; keep the happy path flat.
- **altune** — the house default. Go's `go-code-style` "Reduce Nesting / Eliminate else" rule IS this technique — verified live in `services/go-api/internal/discovery/service/suggest.go` (`Execute` guards `norm == ""`, then the repo error, then the short-circuit, leaving one trailing happy-path call). RN-TS hooks/components: early-return `null`/loading/error states before the main render.
- **Skip when** — N/A here; this is essentially always correct in both stacks.

### Replace Conditional with Polymorphism
- **Smell** — a `switch`/`if-else` on a type or kind field, *repeated* in several places, each arm doing type-specific work.
- **Move** — Fowler's class-subclass version doesn't map (no inheritance in Go or RN-functional). The compositional equivalent: dispatch via interface satisfaction (Go) or a strategy table of function values / per-kind hooks (RN-TS).
- **altune** — Go: define a small interface (the consumer's seam) and let each concrete type satisfy it — `discovery`'s per-provider adapters and per-kind `*Enricher` ports are exactly this; a `map[ResultKind]handler` table also works for a single switch. RN-TS: a `Record<Kind, Component>` lookup or `Record<Kind, () => …>` strategy map instead of `switch (kind)` scattered across files. Apply only when the same switch recurs (Rule of Three) — and extraction to `shared/` still needs 2+ real consumers.
- **Skip when** — the switch appears once and is unlikely to spread; a single local `switch` is clearer than indirection (`go-design-patterns`: don't abstract prematurely).

### Introduce Null Object
- **Smell** — the same `if x == nil { default behavior }` repeated at every call site.
- **Move** — return an object/value that exhibits the default behavior, so callers stop null-checking.
- **altune** — Go: lean on *useful zero values* (`go-safety`) — a no-op implementation of a port (the no-op `EnrichmentCache`/`LyricsCache` when Redis is absent is exactly a Null Object), or return `[]T{}` not `nil` so callers `range` freely. RN-TS: default props / `?? defaultValue` / an empty-but-valid object so components don't branch on `undefined`.
- **Skip when** — `nil`/absence is *semantically distinct* from the default and callers must treat it differently (e.g. "not found" vs "found, empty"); collapsing them hides a real case.

### Introduce Assertion
- **Smell** — code that silently assumes a precondition ("balance is always ≥ 0 here") with no statement of it.
- **Move** — make the assumption explicit as a checked assertion.
- **altune** — Go has no `assert`; encode invariants in the *type* so illegal states are unrepresentable (value objects per `code-quality` — `TrackId`, `SearchQuery` validating in its constructor), or `panic` only for true should-never-happen bugs (`go-design-patterns`: panic is for bugs, not expected errors). Never use a panic where an `error` return belongs. RN-TS: TypeScript's type system + a dev-only `invariant(cond, msg)`; prefer narrowing the type so the impossible state can't compile.
- **Skip when** — the condition is reachable from untrusted input — that's validation returning an `error`/typed result, not an assertion. Don't assert what a user can trigger.
