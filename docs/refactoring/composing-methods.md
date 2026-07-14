---
description: "Refactoring catalog — Composing Methods (Fowler, adapted to altune Go + RN/TS). On-demand reference for /tighten-backend, /tighten-frontend, /improve-codebase-architecture."
source: https://refactoring.guru/refactoring/techniques/composing-methods
---

# Refactoring — Composing Methods

Long functions and tangled locals — the moves here trade a fat body for a set of small, named, deep helpers. The interface (signature) stays narrow; the implementation shrinks behind it.

## When to reach for these
After a review skill names a smell — long function, mixed levels of abstraction, a local that means three things, an opaque boolean expression — it picks the matching technique below and applies the move. This group is the default toolkit when `/tighten-backend` flags a service method over the ~10-line code-quality limit, or `/tighten-frontend` flags a component body doing fetch + transform + render at once.

## Techniques

### Extract Method
- **Smell** — a function does several things; a fragment needs a comment to explain it.
- **Move** — lift the fragment into a named function; the name replaces the comment.
- **altune** — the primary deepening move in both stacks. Go: split a service use case into private helpers — `service/rank.go` `Rank` delegates to `relevanceScore`, `sharesQueryWord`, `hasBrowseableSource`, `rankLess` (verified). RN/TS: pull data orchestration out of a component into a feature hook (`features/<feat>/hooks/`), leaving the component to render. Extraction stays local — `shared/` needs 2+ consumers.
- **Skip when** — the fragment is one trivial expression with a self-evident purpose; extracting only adds an indirection to chase.

### Inline Method
- **Smell** — a function's body is as clear as its name; the indirection earns nothing.
- **Move** — paste the body into callers and delete the function.
- **altune** — collapse a one-line wrapper that just forwards args, or a hook that wraps a single `useQuery` with no added logic. Reverses an over-eager earlier extraction. The deletion test made concrete — if removing it changes nothing but call sites, inline it.
- **Skip when** — the function is a port method, a polymorphic seam (interface satisfaction), or has 2+ callers that benefit from the shared name.

### Extract Variable
- **Smell** — a long expression buries its meaning in operators.
- **Move** — name sub-results in well-named locals before the final expression.
- **altune** — directly enforces `go-code-style`'s "3+ operands → named booleans": `isAdmin := …; isOwner := …; if isAdmin || isOwner`. RN/TS: name a derived render value (`const hasResults = data.length > 0`) instead of inlining it in JSX.
- **Skip when** — Go: the value is used once and `:=` would force an awkward scope, or naming would fight short-circuit evaluation of an expensive check (keep it inline).

### Inline Temp
- **Smell** — a temp just aliases a simple expression, adding a name nobody needs.
- **Move** — replace the temp's references with the expression itself.
- **altune** — drop `count := len(xs); return count` → `return len(xs)`. Usually a precursor to Replace Temp with Query. Minor; apply only when the temp adds no clarity.
- **Skip when** — the temp documents intent (`isEmpty := len(xs) == 0`) or the expression is non-trivial / evaluated more than once.

### Replace Temp with Query
- **Smell** — a local holds a computed value that callers (or other methods) would also want.
- **Move** — extract the computation into its own function; call it instead of storing.
- **altune** — Go: turn a precomputed field into a small method on the struct/aggregate, so the value is derived not stored — fewer fields to keep consistent. RN/TS: replace a `useState` + effect that mirrors derivable data with a plain `const` (or `useMemo`) computed during render — the classic "derived-state" bug fix.
- **Skip when** — the computation is expensive and the temp is a deliberate cache (then it's memoization, not a smell), or it has observable side effects.

### Split Temporary Variable
- **Smell** — one mutable local is reassigned to mean different things across a function.
- **Move** — give each responsibility its own single-purpose variable.
- **altune** — Go favors this hard: prefer one `:=` per concept over reusing a `temp`. A loop accumulator is the legitimate exception. RN/TS: never overload one `let` across render phases — separate consts read cleaner and survive reordering.
- **Skip when** — the variable is a genuine accumulator (loop counter, running total, `strings.Builder`) — single identity, many writes.

### Remove Assignments to Parameters
- **Smell** — a function reassigns its own parameter, hiding the original input.
- **Move** — assign to a fresh local; leave the parameter untouched.
- **altune** — Go passes by value, so reassigning a param is local-only but still misleading — copy to a named local first. The real hazard is slices/maps/pointers: mutating the pointee is caller-visible aliasing (`go-safety`) — clone (`slices.Clone`) before mutating. RN/TS: never mutate a prop or a param object; treat inputs as immutable and derive new values.
- **Skip when** — N/A here — there's no idiomatic case for reassigning a parameter in either stack; the move always applies.

### Replace Method with Method Object
- **Smell** — a function so long its locals are too intertwined to Extract Method.
- **Move** — promote it to its own object whose fields hold the former locals; extract freely against those fields.
- **altune** — Go has no classes but the equivalent is direct: hoist the function into its own small struct (the tangled locals become fields), then break it into methods on that struct — exactly how a use case lives as a `service` struct with injected ports. RN/TS: extract the logic into a dedicated custom hook (or a small reducer) whose closure/state holds the intermediate values; the component calls the hook.
- **Skip when** — Extract Method alone untangles it; reach for this only when shared locals block plain extraction. Don't manufacture a struct/hook for a function that decomposes cleanly.

### Substitute Algorithm
- **Smell** — the algorithm is convoluted; a clearer one exists with the same contract.
- **Move** — replace the whole body with the simpler algorithm in one swap.
- **altune** — the move behind the discovery rank rebuild: a published similarity measure replaced tuned bands/tiers behind the same `Rank` signature (`service/rank.go`). Safe only behind a stable interface with a test/eval gate — here `cmd/discoveryeval` baselines guard the swap. Do the swap whole; don't interleave with extraction.
- **Skip when** — no characterization tests exist to prove behavior is preserved (write them first), or the two algorithms have subtly different contracts (then it's a behavior change, not a refactor).
