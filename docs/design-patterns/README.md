# Design patterns catalog — altune

The 23 Gang-of-Four design patterns (via [refactoring.guru/design-patterns/catalog](https://refactoring.guru/design-patterns/catalog)), adapted to altune's stack: Go (hexagonal, **no classes / no inheritance**) and React Native + TypeScript (function components + hooks, vertical slices). Each pattern carries its **intent**, the **problem** it answers, the canonical **solution** shape, how it **maps in altune** (Go and RN/TS, with verified file references where a real instance exists), and **when to skip it**.

These are **on-demand reference docs**, living under `docs/` rather than `.claude/rules/` — a file in `.claude/rules/` without `paths:` frontmatter loads globally on every session (that's how always-on rules like `mcp-tool-routing.md` work), so on-demand catalogs must live outside that directory to carry zero standing token cost. Skills consult them by name via Read; they don't auto-load on every edit. This catalog is the sibling of [`../refactoring/`](../refactoring/README.md): refactorings are the *moves* (transformations), design patterns are the *target shapes*. They cross-link.

## How to use this

The point is to flip the default from *recall-if-prompted* to *recognize-and-name*. When a skill is designing a seam or reviewing structure, it should **name the specific pattern from this catalog and apply (or consciously reject) it** — turning "this needs polymorphism" into "this is *Strategy* — here's the function-value form, and here's why not *State*." A design choice that names a pattern is grounded; one that doesn't is a vibe.

Consumed by (among others): `/tighten-backend`, `/tighten-frontend`, `/improve-codebase-architecture`, `/feature-plan`, and the `codebase-design` / `domain-modeling` skills. The pattern lexicon (`~/.claude/lexicon/`) is the authoritative deep reference; this catalog is the fast, altune-specific, zero-cost layer.

## Stack caveat (read this first)

GoF is class- and inheritance-centric. altune has **neither** classes (Go) nor class components (RN). So most patterns are **remapped** to their compositional equivalents — **interface satisfaction, struct embedding, function values, functional options** (Go) and **custom hooks, component composition, discriminated unions, strategy tables** (RN/TS) — or marked **N/A** with the reason. The honest remapping is the value: don't force a class-hierarchy pattern onto a language without classes. Several patterns are essentially already dissolved into the architecture (the hexagonal ports/adapters split *is* Adapter + Strategy + Facade) — those files say so plainly rather than inventing a separate object.

**Verdict legend:** ✅ verified live instance in the repo · ◐ conceptual (the shape is idiomatic here but no file is labelled as such) · ⊘ N/A or anti-pattern in this stack (alternative given).

## Creational

| Pattern | Verdict | altune form |
|---------|:------:|-------------|
| [Factory Method](creational/factory-method.md) | ◐ | `New…` constructors; per-provider adapters behind a port |
| [Abstract Factory](creational/abstract-factory.md) | ◐ | rare — ports injected individually, not as families |
| [Builder](creational/builder.md) | ✅ | **functional options** (`consensus.go` `New…` + `With…`) — the mandated constructor form |
| [Prototype](creational/prototype.md) | ⊘ | no `Clone()` idiom — defensive copy via `slices.Clone` / spread |
| [Singleton](creational/singleton.md) | ⊘ | `init()`/globals banned — construct-once in `app.go`, inject |

## Structural

| Pattern | Verdict | altune form |
|---------|:------:|-------------|
| [Adapter](structural/adapter.md) | ✅ | the backbone — `adapters/providers/*` implement `ports/` (note: GoF-Adapter ⊃ hexagonal adapters layer) |
| [Bridge](structural/bridge.md) | ⊘ | no inheritance to escape — DI-via-interface covers it |
| [Composite](structural/composite.md) | ◐ | no recursive domain tree today; RN `children` is composite-by-default |
| [Decorator](structural/decorator.md) | ◐ | same-port wrapper (middleware-style); distinct from Proxy |
| [Facade](structural/facade.md) | ✅ | service-layer use cases front scatter-gather; `app.go` is the root |
| [Flyweight](structural/flyweight.md) | ⊘ | optimization of last resort — not warranted at current scale |
| [Proxy](structural/proxy.md) | ✅ | read-through caches `EnrichmentCache` / `LyricsCache` (no-op when Redis absent) |

## Behavioral

| Pattern | Verdict | altune form |
|---------|:------:|-------------|
| [Chain of Responsibility](behavioral/chain-of-responsibility.md) | ◐ | `net/http` / chi middleware chain |
| [Command](behavioral/command.md) | ◐ | service `Execute` use cases; TanStack `mutate` |
| [Iterator](behavioral/iterator.md) | ◐ | Go 1.23+ `iter.Seq` for lazy/streaming |
| [Mediator](behavioral/mediator.md) | ◐ | the service layer / Zustand stores already mediate — don't add an object |
| [Memento](behavioral/memento.md) | ✅ | playback snapshot for resume-on-reopen (`queueStore.ts`) |
| [Observer](behavioral/observer.md) | ◐ | domain events (`SearchPerformed`…); React render + Query/Zustand on RN |
| [State](behavioral/state.md) | ✅ | `AcquisitionStatus`, `RepeatMode`, the playback `Queue` machine |
| [Strategy](behavioral/strategy.md) | ✅ | function values / interface satisfaction — per-kind `*Enricher` ports, providers |
| [Template Method](behavioral/template-method.md) | ◐ | function-value form `fetch(ctx, parse func(...))` — no abstract base |
| [Visitor](behavioral/visitor.md) | ⊘ | no double-dispatch idiom — type switch / strategy table substitutes |
| [Interpreter](behavioral/interpreter.md) | ⊘ | no DSL — *not on refactoring.guru; included for full-GoF completeness* |

## See also

- [`../refactoring/README.md`](../refactoring/README.md) — Fowler's refactoring catalog (the *moves* that arrive at these shapes)
- `~/.claude/lexicon/` — the pattern lexicon: authoritative deep pattern reference (per-language manifests + full entries under `site/`)
