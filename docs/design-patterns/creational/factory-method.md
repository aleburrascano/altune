# Factory Method — Creational

> GoF creational pattern. Source: https://refactoring.guru/design-patterns/factory-method

**Intent.** Define an interface for creating an object, but let the implementation decide which concrete type to instantiate, so callers depend on the abstraction not the concrete class.

## Problem
Creation logic is scattered as direct `&Concrete{}` constructions throughout the code. Adding a new variant means hunting down and editing every call site, and callers are welded to concrete types they shouldn't know about.

## Solution
Replace direct instantiation with a creation seam — in GoF, a method subclasses override to return a different product. The product type is an interface; the caller works only against that interface and never names the concrete type.

## In altune
**Go:** No subclassing, so the "method a subclass overrides" collapses to **a function value or a small interface defined where consumed**. A `func(cfg) ports.SearchProvider` selected by config, or a registry `map[ProviderName]func() Provider`, is the idiomatic Factory Method. The product is a 1–3-method port (`SearchProvider`); the concrete adapter satisfies it structurally. The composition root (`internal/app/app.go`) is where the selection happens and the concrete is injected.
**RN/TS:** A factory function or hook that returns the right shape from input — `Record<Kind, () => Component>` lookup, or a `useX()` hook that picks a concrete implementation. The component's props are its product interface.
[conceptual] — no single file is *named* a factory; the per-provider adapters behind `ports` ports are the realized shape.

## When to reach for it
- A second concrete implementation of a port appears and the choice is config/runtime-driven (provider selection, cache backend present-or-no-op).
- You want to centralize "which concrete" in the composition root and keep the rest of the code on the interface.

## When to skip it
- One implementation, one caller — "discover interfaces, don't design them." A factory returning the only concrete is indirection for its own sake; just call `New…` directly.
- The variation is a single boolean — a `switch` at the one call site beats a registry until the Rule of Three.

## Related
- Patterns: [[abstract-factory]] (Factory Method makes one product; Abstract Factory a *family*), [[builder]] (step-by-step construction vs one-shot creation), [[prototype]] (clone vs construct)
- Refactoring moves: `../../refactoring/simplifying-method-calls.md` — *Replace Constructor with Factory Method*; `../../refactoring/simplifying-conditional-expressions.md` — *Replace Conditional with Polymorphism*
- Project rules: `../../../.claude/rules/backend/go-design-patterns.md` (functional options, no `init()`), `../../../.claude/rules/backend/go-structs-interfaces.md` (define interfaces where consumed)
