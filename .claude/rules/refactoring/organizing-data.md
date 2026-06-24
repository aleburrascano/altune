---
description: "Refactoring catalog — Organizing Data (Fowler, adapted to altune Go + RN/TS). On-demand reference for /tighten-backend, /tighten-frontend, /improve-codebase-architecture."
source: https://refactoring.guru/refactoring/techniques/organizing-data
---

# Refactoring — Organizing Data

Primitive obsession, leaky field access, and type codes that drive behavior with `switch` — the moves here turn raw data into types with a contract, so the invariant lives at the seam instead of at every call site.

## When to reach for these
After a review skill flags "raw string/int for a domain concept", "public mutable field", "magic number", "exported collection mutated by callers", or "`switch` on a status int scattered across files" — it names the technique below and applies the move. This group is the bread-and-butter of `/tighten-backend` (domain value objects, enum modeling) and `/tighten-frontend` (discriminated unions over nullable fields, readonly props).

## Techniques

### Self Encapsulate Field
- **Smell** — code reads/writes a struct field directly when computed access or validation is wanted.
- **Move** — route access through getter/setter (Go) or a hook/accessor (RN-TS).
- **altune** — Go domain VOs already do this: unexported fields + exported getters (`TrackId.UUID()`, `TrackId.String()`). The deletion test: if removing the getter forces no call-site change, the field was never encapsulated.
- **Skip when** — a plain DTO/wire struct with public tagged fields and no invariant. Encapsulating it is ceremony.

### Replace Data Value with Object
- **Smell** — a primitive (`string`, `int`) carries domain meaning and is starting to grow validation/behavior around it.
- **Move** — promote it to a value object.
- **altune** — the core domain-layer rule: "wrap domain primitives in value objects" (`TrackId`, `PlaylistId`, `UserId`). In RN-TS, a branded type or a small `type` + parser. This is the deepest move in the group — one named type absorbs scattered validation.
- **Skip when** — the value has no invariant and one consumer (YAGNI). A `name string` with no rules stays a string.

### Change Value to Reference
- **Smell** — many identical copies of a conceptually-shared entity, edits to one should reflect in all.
- **Move** — replace duplicated values with a single shared reference (identity, looked up by id).
- **altune** — this is the entity-vs-VO distinction. A `Track` is referenced by `TrackId` across playlists, not copied — see `Playlist` holding `PlaylistTrack{track_id}` rather than embedded track data. The aggregate boundary already enforces it.
- **Skip when** — the data is genuinely a value (immutable, equality-by-attributes). Don't add identity to `RepeatMode`.

### Change Reference to Value
- **Smell** — a small, immutable, frequently-passed reference type adds lifecycle/identity overhead for no gain.
- **Move** — make it an immutable value compared by attributes.
- **altune** — Go VOs are this by construction (unexported fields, equality by value, no shared mutable state). `MBEnrichment`, `DiscogsEnrichment` are immutable read surfaces — values, not entities.
- **Skip when** — the type needs shared identity or is mutated in place. Then it's an entity; keep the reference.

### Replace Array with Object
- **Smell** — an array/tuple whose positions mean different things (`["track", title, 2026]`).
- **Move** — replace with a struct/object with named fields.
- **altune** — positional data is a code-quality violation here (Go composite literals MUST use field names). Use a struct (`SourceRef{Provider, ExternalID, URL}`) or a TS object/discriminated union, never an index-keyed array.
- **Skip when** — the array is genuinely homogeneous and ordered (a list of tracks). It's a collection, not a disguised record.

### Duplicate Observed Data
- **Smell** — domain data trapped inside UI/handler code, needing to live in two places that stay in sync.
- **Move** — pull the domain data into its own layer with one-way sync.
- **altune** — the hexagonal/vertical-slice boundary already mandates this: handlers hold no domain state; RN screens hold no business logic (extracted to `hooks/`, server state in TanStack Query). The "observer sync" is React's render + query cache, not hand-rolled.
- **Skip when** — N/A as a manual move — the architecture forbids the smell upstream. If you find it, the fix is the layer boundary, not a sync mechanism.

### Change Unidirectional Association to Bidirectional
- **Smell** — object A holds B, but B now also needs to reach A.
- **Move** — add the back-reference.
- **altune** — resist this. Bidirectional links between aggregates violate the dependency rule (no cross-aggregate transactions; coordinate via domain events). In RN, back-pointers between components/stores create render cycles.
- **Skip when** — almost always here. If B needs A, pass A's id and look it up, or emit an event. N/A as a default move — it fights the inward-only dependency rule.

### Change Bidirectional Association to Unidirectional
- **Smell** — a two-way link where one direction is unused, adding coupling and lifecycle complexity.
- **Move** — drop the unused reference.
- **altune** — this one we *want*: it tightens toward the inward-only rule. If a domain type holds a pointer back to a service/handler, sever it. Apply the deletion test on the back-reference.
- **Skip when** — both directions have real consumers (rare across a clean boundary).

### Replace Magic Number with Symbolic Constant
- **Smell** — a literal with non-obvious meaning (`0.92`, `30`, `50`).
- **Move** — name it as a constant at package/module scope.
- **altune** — Go: package-level `const` (MixedCaps, not ALL_CAPS) — e.g. the JW dedup thresholds in discovery. RN-TS: `as const` token or a named const, never an inline literal (theme values MUST be tokens). Constants are named by *role*, not value (`DefaultLimit`, not `Limit50`).
- **Skip when** — the number is self-evident in context (`for i := range 3`, array index `0`). Naming `1` as `One` is noise.

### Encapsulate Field
- **Smell** — a public/exported mutable field anyone can write, bypassing invariants.
- **Move** — unexport it, expose controlled access.
- **altune** — Go: lowercase the field, add a getter; aggregates validate on every state change via methods, never direct construction. RN-TS: `readonly` props/fields (see `PlaybackState` — every field `readonly`) so consumers can't mutate state objects.
- **Skip when** — wire/DTO structs and pure prop bags where exported fields *are* the interface.

### Encapsulate Collection
- **Smell** — a getter hands out the live slice/array; callers mutate internal state behind the owner's back.
- **Move** — return a copy (or read-only view); expose `Add`/`Remove` that enforce invariants.
- **altune** — Go safety rule: exported accessors return defensive copies (`slices.Clone`); the `Playlist` aggregate owns ordering/contiguity invariants and exposes add/remove, not the raw `[]PlaylistTrack`. RN-TS: `readonly T[]` and treat state arrays as immutable (spread to update).
- **Skip when** — an internal-only collection with a single owner and no invariant to protect.

### Replace Type Code with Class
- **Smell** — a bare `int`/`string` type code with no behavior but no type safety either.
- **Move** — give the code its own type with methods.
- **altune** — exactly the Go enum-as-named-type pattern. `AcquisitionStatus int` with `iota` + a `String()` method and a `ParseAcquisitionStatus` — verified at `services/go-api/internal/catalog/domain/track.go:37`. RN-TS: a string-literal union (`type RepeatMode = 'off' | 'all' | 'one'` in `apps/mobile/src/shared/playback/types.ts:34`) instead of a bare `string`.
- **Skip when** — N/A — this is the default for every status/kind/mode in the codebase. The smell *is* the unmodeled primitive.

### Replace Type Code with Subclasses
- **Smell** — a type code where each value implies different *behavior* (OO: polymorphic subclasses).
- **Move** — (OO) one subclass per code, override behavior.
- **altune** — N/A as written — no class inheritance in Go or RN-functional. The compositional equivalent: a `switch` on the typed code with one branch per value (`AcquisitionStatus.String()`), or strategy-via-function-value / interface satisfaction. For RN, behavior keyed off a discriminated union's tag.
- **Skip when** — the code drives no behavior (then it's Replace Type Code with Class). Don't manufacture an interface for a single behavior — wait for 2+ implementations.

### Replace Type Code with State/Strategy
- **Smell** — behavior varies by a type code that *changes at runtime* (subclassing won't fit a mutable field).
- **Move** — extract a state/strategy object selected by the code.
- **altune** — Go: a `map[Code]func(...)` or a small interface with per-variant implementations, injected (DI via constructor). RN-TS: a strategy hook or a lookup table of handlers keyed by the union tag. Reach for it only when the `switch` recurs in 3+ places (Rule of Three).
- **Skip when** — one `switch` in one place. A lookup table for a single call site is over-abstraction (KISS).

### Replace Subclass with Fields
- **Smell** — (OO) subclasses that differ only by constant return values.
- **Move** — collapse to one type with data fields holding those constants.
- **altune** — N/A — no subclasses to collapse. The native equivalent already in use: a table of structs / a `map[Code]Config` instead of variant types. If you ever model variants as separate Go structs that differ only in constants, fold them into one struct with a config field.
- **Skip when** — variants differ in *behavior*, not just constants (then it's State/Strategy territory).
