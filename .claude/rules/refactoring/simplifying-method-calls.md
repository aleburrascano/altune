---
description: "Refactoring catalog — Simplifying Method Calls (Fowler, adapted to altune Go + RN/TS). On-demand reference for /tighten-backend, /tighten-frontend, /improve-codebase-architecture."
source: https://refactoring.guru/refactoring/techniques/simplifying-method-calls
---

# Refactoring — Simplifying Method Calls

The interface is the cost. These moves attack the *call site* — names that lie, parameter lists that leak, query/command tangles, error channels that fight the language — so a function or component is cheaper to call than to misuse.

## When to reach for these
After a review skill flags a confusing signature, a boolean/flag parameter, a getter with side effects, or a fragile constructor, it names the technique below and applies the move. This group is the toolkit for *shallow interfaces over working bodies* — the body is fine, the seam is wrong.

## Techniques

### Rename Method
- **Smell** — name doesn't describe what the function does; readers open the body to learn the interface.
- **Move** — rename to the effect, not the mechanism.
- **altune** — Go: use `mcp__serena__rename_symbol` so all call sites + tests move atomically; honor canonical names (`String()` not `ToString()`) and anti-stutter (`user.New()` not `user.NewUser()`). RN-TS: rename a hook to its return contract — `useArtistDetail` over `useArtistData`. A method's name IS half its interface; an honest name is a deeper module.
- **Skip when** — the name is exported across a context boundary with external consumers (wire/API names) — those rename via deprecation, not in place.

### Add Parameter
- **Smell** — the function lacks data it now needs and reaches for a global/field instead.
- **Move** — pass the datum in.
- **altune** — last resort. Go: `ctx` is always first; once you're at 4+ params, stop and reach for Introduce Parameter Object or an options struct (per `go-code-style.md`). Domain stays pure — pass the value, never an adapter.
- **Skip when** — the value is derivable from an argument the function already holds — that's Replace Parameter with Method Call, not a new param.

### Remove Parameter
- **Smell** — a parameter is never read in the body — dead weight on every call site.
- **Move** — delete it; let the compiler/`go vet` find the call sites.
- **altune** — Go's unused-param tolerance hides these; `find_referencing_symbols` confirms it's truly dead before removal. RN-TS: drop an unused prop — it shrinks the component's interface and the deletion test passes cleanly.
- **Skip when** — the parameter satisfies an interface/port signature shared by other implementations — removing it breaks satisfaction. Keep it, name it `_`.

### Separate Query from Modifier
- **Smell** — a function returns a value *and* mutates state — callers can't read without side effects.
- **Move** — split into a pure query and a void command (Command-Query Separation).
- **altune** — directly the `code-quality.md` "Tell, Don't Ask" line. Go: a repo method that fetches-and-marks should be `Get(...)` + `Mark(...)`. RN-TS: a hook returning data while firing a mutation as a side effect of render is a bug — split into a selector + an explicit `mutate()`.
- **Skip when** — the atomicity *is* the contract (e.g. `LoadOrStore`, a dedup-on-write repo). Name it so the dual nature is visible.

### Parameterize Method
- **Smell** — several near-identical functions differing only by a constant.
- **Move** — collapse into one function taking that constant as a parameter.
- **altune** — Go: three `lookupDeezer/lookupLastfm/lookupMB` bodies that differ by endpoint → one `lookup(provider)`; matches the per-provider `ConsensusProvider` shape in `service/consensus.go`. RN-TS: three styled variants → one component with a `variant` prop.
- **Skip when** — the bodies diverge beyond the constant — parameterizing forces conditionals that re-tangle them. Then it's the opposite move (below).

### Replace Parameter with Explicit Methods
- **Smell** — a function branches on an enum/flag parameter, running a different limb per value — the inverse of the smell above.
- **Move** — extract each limb as its own named function; delete the discriminator.
- **altune** — kills boolean-trap calls. `render(true)` → `renderActive()` / `renderArchived()`. Go: a `switch kind` that dispatches to wholly unrelated logic is usually N small methods behind a tiny interface (strategy via function values or interface satisfaction).
- **Skip when** — the parameter comes from data at runtime (a wire `ResultKind`, user input) — you can't pick the method at the call site, so keep the switch.

### Preserve Whole Object
- **Smell** — caller pulls several fields off an object only to pass them as separate parameters.
- **Move** — pass the whole object.
- **altune** — Go: `BuildConsensus(artist.Name, artist.MBID, artist.Aliases)` → pass the `ArtistIdentityProfile` (the codebase already does this — see `service/consensus.go:98`). Shrinks the signature and survives the struct gaining a field. RN-TS: pass the `track` object, not seven destructured props.
- **Skip when** — the function would then import a heavy type just to read two fields, or the domain layer would gain a dependency it shouldn't — pass the primitives and keep the seam thin.

### Replace Parameter with Method Call
- **Smell** — caller computes a value and passes it, but the callee could compute it itself.
- **Move** — drop the parameter; call the query inside.
- **altune** — Go: don't pass `now time.Time` if the function can read the clock — *unless* injecting it aids testability (then keep it; testability outranks param count). RN-TS: a hook that takes a value already available from a context it consumes should read the context.
- **Skip when** — the value is an injected dependency (clock, config) — explicit injection is the project's DI rule, not a smell.

### Introduce Parameter Object
- **Smell** — the same clump of parameters recurs across functions.
- **Move** — bundle them into a named struct/type.
- **altune** — Go: 4+ params → an options struct or input struct (`go-code-style.md` mandates this); a recurring `(limit, offset, kinds)` clump becomes a `SearchQuery` value object. Naming the clump often surfaces a missing domain concept. RN-TS: a recurring prop trio → a typed object prop or a small context.
- **Skip when** — the params don't travel together elsewhere (single call site) — YAGNI; one struct for one caller is ceremony.

### Remove Setting Method
- **Smell** — a field gets a setter though it should be fixed at construction.
- **Move** — set it in the constructor; delete the setter.
- **altune** — Go has no setters idiomatically — build immutable structs via `New…` + functional options (`WithMBAuthority` in `service/consensus.go:112`); a later `SetMB()` would be the smell. RN-TS: a piece of state that only ever derives from props shouldn't have a `setX` — compute it, don't store-and-set.
- **Skip when** — the field genuinely changes over the object's life (mutable runtime state like `Queue` current index) — it earns its setter.

### Hide Method
- **Smell** — an exported function nobody outside the package/module calls — needless public surface.
- **Move** — unexport it.
- **altune** — Go: "unexport aggressively — exporting later is free, unexporting is breaking" (`go-naming.md`). `find_referencing_symbols` proves no external caller, then lowercase the identifier. RN-TS: a helper exported from a feature but used only within it — make it module-private; cross-feature import is forbidden anyway until 2+ consumers.
- **Skip when** — it's part of a port interface or the feature's intended public API — exported by design.

### Replace Constructor with Factory Method
- **Smell** — construction does real work (validation, branching on type) beyond field assignment.
- **Move** — route construction through a named factory.
- **altune** — Go has no constructors — `New…` *is* the factory, and the rule is functional options for anything beyond field-setting (`go-design-patterns.md`); a `New…` that validates should return `(*T, error)`. Use `NewTypeName` only when a package builds multiple types. RN-TS: a factory hook/function that picks the right component shape from input.
- **Skip when** — construction is pure field assignment — a plain struct literal or trivial `New` is deeper than a factory wrapper.

### Replace Error Code with Exception
- **Smell** — a function signals failure with a magic return value (`-1`, `""`, `nil`-as-error) the caller must remember to check.
- **Move** — use the language's real error channel.
- **altune** — Go inverts Fowler: the *idiomatic* channel is the returned `error`, not a panic. The real move here is **magic value → explicit `error`** — return `(T, error)`, wrap with `fmt.Errorf("…: %w", err)`, never `panic` for expected failure (`go-error-handling.md`). RN-TS: return a typed result/throw for the error boundary; don't encode failure as a sentinel value.
- **Skip when** — failure is a normal domain outcome, not an error — then it's the move below.

### Replace Exception with Test
- **Smell** — code throws/errors where a cheap precondition check would do; exceptions used for control flow.
- **Move** — test the condition first; reserve errors for the genuinely exceptional.
- **altune** — Go: guard-clause up front rather than recovering a panic — `if count == 0 { return 0, nil }` beats dividing and recovering; "no rows" is `errors.Is(err, sql.ErrNoRows)` → a domain `(value, exists, false-error)` triple, not an error (`go-database.md`). A definitive "no lyrics for region" returns an empty value + nil error (negative-cacheable), per the `LyricsProvider` contract. RN-TS: check `data == null` before use rather than catching the deref.
- **Skip when** — the test would race the operation (TOCTOU) or duplicate expensive work the call already does — let it fail and handle the error.
