---
description: "Refactoring catalog — Dealing with Generalization (Fowler, adapted to altune Go + RN/TS). On-demand reference for /tighten-backend, /tighten-frontend, /improve-codebase-architecture."
source: https://refactoring.guru/refactoring/techniques/dealing-with-generalization
---

# Refactoring — Dealing with Generalization

Duplication across siblings and abstractions in the wrong place — these moves shuffle behavior up, down, or sideways to put each piece where exactly its consumers can see it. Fowler frames them around class inheritance; altune has none, so most remap to **interface satisfaction, struct embedding, function values, and custom hooks** — or don't apply at all.

## When to reach for these
After a review skill flags duplicated logic across parallel implementations (two providers, two repos, two screens), an interface that's too fat or too thin for its callers, or an abstraction whose locality is wrong — it names the technique below and applies the compositional equivalent. The honest remapping is the value: don't force a class-hierarchy move onto a language without classes.

## Techniques

### Pull Up Field
- **Smell** — sibling structs each carry the same field.
- **Move** — hoist the shared field to a common type they all embed.
- **altune** — Go: extract a small shared struct and **embed** it (`type base struct{ logger *slog.Logger }`) rather than copy the field into each provider/repo. RN-TS: lift shared state into a parent component or a shared hook's return.
- **Skip when** — the field is shared by coincidence, not concept. Two fields named `id` of unrelated types are not one field. A little copying beats a wrong abstraction.

### Pull Up Method
- **Smell** — sibling implementations contain a near-identical method.
- **Move** — make them identical, then hoist to a shared seam.
- **altune** — Go: move the common logic into a free function or an embedded base type's method; the providers call it. RN-TS: extract a shared **custom hook** or pure helper in the feature's `hooks/`/`lib/`. Promotion to `shared/` still needs 2+ real consumers.
- **Skip when** — the bodies only look alike. If unifying them forces a flag parameter to paper over differences, leave them apart.

### Pull Up Constructor Body
- **Smell** — sibling constructors repeat the same setup.
- **Move** — factor the common construction into one place the others delegate to.
- **altune** — Go: a shared `newBase(...)` helper or an embedded base initialized once; functional-options defaults (`go-design-patterns.md`) already centralize this. RN-TS: a factory hook or shared initializer.
- **Skip when** — constructors differ in validation order or invariants — forcing a shared body hides per-type rules.

### Push Down Method
- **Smell** — a method on a shared type is used by only one consumer.
- **Move** — relocate it down to that single consumer; shrink the shared surface.
- **altune** — Go: drop it from the wide interface; let the one provider expose it as an **optional capability interface** the caller type-asserts for — exactly `StructuredSearcher` in `services/go-api/internal/discovery/ports/ports_search.go` (verified), satisfied only by providers that support split artist+track queries. RN-TS: move the handler from a shared hook into the one screen that uses it.
- **Skip when** — N/A as inheritance, but the ISP intent is core: a port with one niche method used by one impl belongs as its own interface.

### Push Down Field
- **Smell** — a field on a shared type matters to only one consumer.
- **Move** — push it down to where it's actually read.
- **altune** — Go: remove it from the embedded base; add it to the one concrete struct that needs it — restores locality. RN-TS: move the state out of a shared hook into the single feature owning it.
- **Skip when** — N/A here as a hierarchy move — there is no superclass field to demote, only embedded-struct or shared-hook bloat. Same instinct: keep state next to its only reader.

### Extract Subclass
- **Smell** — a type carries fields/branches used only in certain cases.
- **Move** — split the special case into its own variant.
- **altune** — no subclassing. Go equivalent: a discriminated set behind a small interface, or distinct structs satisfying one port — make illegal states unrepresentable (`go-safety.md`) instead of a `kind` flag gating fields. RN-TS: split a multi-mode component into variant components sharing a hook.
- **Skip when** — the "special case" is one boolean. A flag is cheaper than a type explosion until the branches multiply.

### Extract Superclass
- **Smell** — two types share fields and behavior with no common abstraction.
- **Move** — create the shared parent and pull the commonality up.
- **altune** — no superclass. Go: **define the interface where it's consumed** (`go-structs-interfaces.md`) once there are 2+ implementations, or extract a shared embedded struct. RN-TS: extract a shared hook/component once 2+ features consume it. The ports package (`ports_search.go`, verified) is this done right — small consumer-defined interfaces, providers satisfy them structurally with no declared inheritance.
- **Skip when** — only one implementation exists. "Don't design with interfaces, discover them" — premature abstraction adds indirection with no payoff.

### Extract Interface
- **Smell** — multiple callers depend on the same slice of a type's surface.
- **Move** — name that slice as its own interface; callers depend on it.
- **altune** — the most natural move here. Go: carve a fat interface into 1–3-method ports per consumer (ISP) — `ports_search.go` (verified) already splits `SearchProvider`, `AlbumContentProvider`, `ArtistContentProvider`, `RelatedTracksProvider` rather than one god-port. RN-TS: a component's props **are** its interface — narrow them to what each caller passes.
- **Skip when** — a single caller uses the whole surface. An interface with one impl and one caller is indirection for its own sake.

### Collapse Hierarchy
- **Smell** — a layer adds a name but no behavior.
- **Move** — merge it away; delete the empty seam.
- **altune** — apply the **deletion test**: a one-impl interface, a wrapper struct that only forwards, a hook that only re-exports another — inline it. Go: drop the redundant interface, return the concrete type. RN-TS: collapse a pass-through component into its parent.
- **Skip when** — the seam is a genuine test/DI boundary (a port with an in-memory fake) or a planned 2nd-impl seam with the consumer already committed.

### Form Template Method
- **Smell** — siblings run the same step sequence with a few differing steps.
- **Move** — fix the skeleton once; parameterize the varying steps.
- **altune** — no abstract methods. Go: a function taking the varying steps as **function values** (strategy) — `fetch(ctx, parse func(...))` — keeps the skeleton in one place. RN-TS: a hook owning the flow, taking callbacks/render-props for the variable parts; or `children`/render-prop composition.
- **Skip when** — only one step actually varies and there are two callers. Two near-copies can be clearer than a callback-threaded skeleton — Rule of Three.

### Replace Inheritance with Delegation
- **Smell** — a subtype inherits a parent but uses only part of it (or fights it).
- **Move** — hold the other as a field; delegate the methods you need.
- **altune** — Go already favors this: prefer a **named field** over embedding when you only need a few methods (`go-structs-interfaces.md`) — `has-a`, not `is-a`. RN-TS: compose hooks/components by calling/wrapping, not by extending.
- **Skip when** — N/A as a fix (no inheritance to replace) — but it's the default to *reach for first*: embed only to deliberately promote the full API, otherwise delegate.

### Replace Delegation with Inheritance
- **Smell** — a type forwards nearly every method to a delegate, all boilerplate.
- **Move** — inherit to drop the forwarding methods.
- **altune** — N/A as classic inheritance. The Go analogue is **embedding**: if a struct forwards almost the whole API of a field, embed that field to promote its methods and delete the wrappers. Use only when you truly want the full surface exposed.
- **Skip when** — you forward only a *subset*, or you're deliberately narrowing/renaming the surface. Promoting the full API then leaks methods you meant to hide — embedding is all-or-nothing.
