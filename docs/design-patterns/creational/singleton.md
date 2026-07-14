# Singleton — Creational

> GoF creational pattern. Source: https://refactoring.guru/design-patterns/singleton

**Intent.** Ensure a class has exactly one instance and provide a global access point to it.

## Problem
Some resource should exist once (a DB pool, a config) and code everywhere needs to reach it. A plain constructor can't enforce single-instance; a bare global is uncontrolled.

## Solution (and why it's an anti-pattern here)
GoF hides the constructor and exposes a static lazy `getInstance()`. It is widely criticized: it violates SRP (manages lifecycle *and* provides global access), masks coupling (components reach a hidden global instead of declaring dependencies), and is hostile to testing (the static instance can't be swapped or mocked). Multithreading needs special care on top.

## In altune
**Go:** **Explicitly N/A — the project's answer replaces it.** `init()` and mutable package globals are **banned** (`go-design-patterns.md`, `go-dependency-injection.md`). The single-instance need is met by *create-once-in-the-composition-root-and-inject*: build the one DB pool / cache / config in `internal/app/app.go` and pass it through constructors. This delivers single-instance + testability (inject a fake) without a global access point. If a true once-only init is unavoidable (lazy, concurrency-safe), use `sync.Once` on an injected struct — never a package-level singleton.
**RN/TS:** N/A as the classic pattern — a single store/client is created once and provided via a React context or a module-scoped instance imported where needed (the queue store, the api-client). Provider + hook gives the "one instance" with explicit, mockable access — not a hidden global getter.
Project answer verified by rule: single instances live in `internal/app/app.go` and are injected; no `init()`/mutable-global singletons.

## When to reach for it
- Effectively never as GoF Singleton. The legitimate underlying need ("one instance, shared") → construct once at the composition root, inject everywhere.
- `sync.Once` for genuinely-lazy one-time initialization of an *injected* dependency (Go 1.21+: `OnceValue`).

## When to skip it
- Always skip the static-`getInstance` form — it's the masked-coupling, untestable shape the project bans.
- Skip even the injected-singleton instinct when the value is cheap and stateless — just construct it where used; "one instance" isn't a goal in itself.

## Related
- Patterns: [[abstract-factory]] / [[builder]] / [[prototype]] (any can be *implemented* as a singleton in GoF — here all are injected once instead), [[factory-method]] (the composition root is the single place that constructs concretes)
- Refactoring moves: `../../refactoring/organizing-data.md` — *Change Value to Reference* (shared identity via id lookup, not a global), *Encapsulate Field*; `../../refactoring/moving-features-between-objects.md` — *Hide Delegate*
- Project rules: `../../../.claude/rules/backend/go-dependency-injection.md` (no globals/service locator; composition root only), `../../../.claude/rules/backend/go-design-patterns.md` (avoid `init()` and mutable globals), `../../../.claude/rules/backend/go-concurrency.md` (`sync.Once` for lazy init)
