# Abstract Factory — Creational

> GoF creational pattern. Source: https://refactoring.guru/design-patterns/abstract-factory

**Intent.** Produce families of related objects through one factory interface, without binding the client to their concrete classes — and guaranteeing the produced objects are mutually compatible.

## Problem
You need several related objects that must match as a set (a "Modern" vs "Victorian" furniture suite), and you want to add whole new families without rewriting client code. Instantiating each concrete directly lets mismatched variants slip through and couples the client to every concrete.

## Solution
Declare an abstract product interface per product type, and one factory interface with a creation method per product. Each concrete factory produces one internally-consistent family. The client holds the factory interface and the product interfaces only — swapping the factory swaps the entire family at once.

## In altune
**Go:** A factory interface with 2+ related creation methods (`type ProviderSet interface { Search() ports.SearchProvider; Enrich() ports.MetadataEnricher }`), each concrete satisfying it for one backend family. Honest caveat: this is **rare and usually premature here** — the codebase favors injecting the individual ports separately at the composition root (`internal/app/app.go`) over bundling them into a family factory. Reach for it only when the variants must genuinely vary *together*.
**RN/TS:** A theme/platform provider object returning a coherent set of primitives — a design-system `tokens` object (colors + spacing + typography that must match) consumed via context is the closest real shape. One provider swaps the whole visual family.
[conceptual] — no realized family-factory in the repo; ports are wired individually.

## When to reach for it
- 2+ products that must stay consistent as a *set*, with 2+ interchangeable families (e.g. a "stub providers" family vs a "live providers" family for eval harnesses).
- Adding a new family should require no edits to client code — only a new concrete factory.

## When to skip it
- The products don't actually have to match — then they're independent ports; inject them separately (the altune default). Bundling unrelated creation into one fat factory violates ISP.
- Only one family exists — YAGNI. Start with direct wiring; extract the factory interface when the second family is real.

## Related
- Patterns: [[factory-method]] (Abstract Factory is often built *from* several Factory Methods), [[builder]] (Builder constructs one complex object step-by-step; Abstract Factory returns finished families immediately), [[prototype]] (a factory can clone its products), [[singleton]] (a concrete factory is often a single injected instance)
- Refactoring moves: `../../refactoring/dealing-with-generalization.md` — *Extract Interface*; `../../refactoring/simplifying-method-calls.md` — *Replace Constructor with Factory Method*
- Project rules: `../../backend/go-structs-interfaces.md` (small interfaces, accept interfaces / return structs), `../../backend/go-dependency-injection.md` (wire at the composition root only)
