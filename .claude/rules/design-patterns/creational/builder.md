# Builder — Creational

> GoF creational pattern. Source: https://refactoring.guru/design-patterns/builder

**Intent.** Construct a complex object step by step, letting the same construction process yield different representations and keeping optional configuration out of a monstrous constructor.

## Problem
An object has many fields, several optional, with nested or validated configuration. A constructor with 9 positional parameters (the "telescoping constructor") is unreadable and breaks on reorder; scattering the setup across call sites is worse.

## Solution
Move construction into a separate builder that exposes one step per piece of configuration, accumulating state and producing the finished object on a final `build` call. Steps can validate, default, and run in any order; a Director can encapsulate a common step sequence.

## In altune
**Go:** This is the pattern with a **first-class verified instance — functional options**, the house default for any constructor beyond plain field-setting (`go-design-patterns.md`). Each `With…` option is a build step; the variadic `New…` is the director that applies defaults then the steps. Verified: `NewConsensusService(providers, opts...)` at `services/go-api/internal/discovery/service/consensus.go:107`, with the build step `WithMBAuthority` at `consensus.go:103`. Options that can fail should return an `error` — validation at construction, not at runtime. Reach for a classic struct-builder only when steps need ordering/validation *between* steps; otherwise options win.
**RN/TS:** Rarely needed — TS object literals + optional fields + defaults already solve telescoping constructors. A factory hook assembling a complex config object is the nearest equivalent. Mostly N/A; don't import a builder where a typed options object reads cleaner.
Verified instance: `services/go-api/internal/discovery/service/consensus.go:107` (functional-options builder).

## When to reach for it
- A Go constructor crosses ~4 params or grows optional/validated config — switch to functional options immediately (it's the mandated form).
- Construction needs validation *between* steps (the one case for an explicit struct-builder over options).

## When to skip it
- Pure field assignment with no optionals — a plain struct literal or a 1–2 arg `New…` is deeper than any builder.
- RN/TS where an options object with defaults already does the job — a builder adds ceremony with no payoff (KISS).

## Related
- Patterns: [[factory-method]] / [[abstract-factory]] (those return products immediately; Builder defers and assembles in steps — designs often *evolve* from Factory toward Builder as construction grows), [[prototype]] (clone a pre-built object vs build it again), [[singleton]] (the built object may be a single injected instance)
- Refactoring moves: `../../refactoring/simplifying-method-calls.md` — *Introduce Parameter Object*, *Replace Constructor with Factory Method*, *Remove Setting Method*; `../../refactoring/composing-methods.md` — *Replace Method with Method Object*
- Project rules: `../../backend/go-design-patterns.md` (functional options — preferred constructor form), `../../backend/go-code-style.md` (4+ params → options struct)
