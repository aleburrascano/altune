# Prototype — Creational

> GoF creational pattern. Source: https://refactoring.guru/design-patterns/prototype

**Intent.** Create new objects by cloning an existing instance rather than constructing from scratch, without coupling the cloning code to the object's concrete class.

## Problem
You need a copy of an object but can't reach its private fields from outside, and you only know its interface, not its concrete type. Re-running expensive construction or configuration to get an equivalent object is wasteful and duplicative.

## Solution
Give the object itself a `clone` method (declared on a common interface); each class knows how to copy its own internals. Keep a registry of pre-configured prototypes and clone them instead of building new instances.

## In altune
**Go:** **Largely N/A as a named pattern.** Go has no class-coupled copy problem — value types (structs of values, arrays) copy on assignment for free, and the idiomatic deep-copy tools are `slices.Clone` / `maps.Clone` (see `go-safety.md`'s defensive-copy rule). The honest remapping: returning a defensive copy from an accessor, or `cfg2 := cfg1` on an immutable value object, *is* Prototype done the Go way — no `Clone()` interface required. Add an explicit `Clone()` method only for a struct with reference-type fields (slices/maps/pointers) that callers must copy without sharing backing arrays.
**RN/TS:** N/A as a pattern — immutable update is the idiom: `{ ...obj, field }` / `structuredClone(obj)` to derive a new value. State is treated as immutable everywhere; "clone then tweak" is spread syntax, not a registered prototype.
[conceptual] — realized only as defensive copying (`slices.Clone`), never as a `Clone()` interface in the repo.

## When to reach for it
- A struct with mutable reference fields escapes via an exported accessor — return a deep copy (`slices.Clone` the slice fields) so callers can't mutate your internals. This is the only live use.
- You hold a costly-to-build template and want cheap variants — copy the value, override the few differing fields.

## When to skip it
- Almost always, as a formal pattern. A plain value copy or spread already does it; a `Clone()` interface is ceremony Go and TS don't need.
- When absence/identity matters — copying an entity that should be referenced by id breaks the aggregate boundary (`organizing-data.md` — *Change Value to Reference*).

## Related
- Patterns: [[factory-method]] (construct via a seam vs copy an instance — Factory Method often evolves toward Prototype for flexibility), [[abstract-factory]] (a factory may clone its prototypes), [[singleton]] (a prototype *registry* is sometimes a single instance)
- Refactoring moves: `../../refactoring/organizing-data.md` — *Encapsulate Collection* (return `slices.Clone`), *Change Reference to Value*; `../../refactoring/composing-methods.md` — *Remove Assignments to Parameters* (clone before mutating aliased input)
- Project rules: `../../../.claude/rules/backend/go-safety.md` (defensive copying, slice-aliasing trap), `../../../.claude/rules/backend/go-data-structures.md` (`slices.Clone` / `maps.Clone` copy semantics)
