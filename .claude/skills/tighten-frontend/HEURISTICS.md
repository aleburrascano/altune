# Heuristics

The vocabulary and probes the fan-out agents apply. Pairs with `/codebase-design` (module, interface, depth, seam, leverage, locality, the deletion test) — a component's props _are_ its interface, a hook's signature _is_ its interface. This file adds the frontend coupling/cohesion smells and the tooling. Cite the rule or test behind every finding.

Findings are of two kinds. **Measured** — exact, tool-computed, complete every run (below). **Judgment** — needs the agent's eye (the smells and tests that follow). Never eyeball a measured axis; never tool-stamp a judgment one.

## Measured axes — compute, don't eyeball

`fallow` (Rust-native, deterministic, zero-config, already installed) is the engine — run from `apps/mobile/`. Its own shipped skill (`node_modules/fallow/skills/fallow/SKILL.md`) is the authority on invocation and edge cases; the essentials:

| Axis | Command |
|---|---|
| Unused code / dead exports / dependency hygiene | `fallow dead-code` |
| Duplication | `fallow dupes` |
| Complexity / maintainability / hotspots | `fallow health` |
| Circular deps + **architecture-boundary violations** (cross-feature imports) | `fallow dead-code` |
| All three at once | `fallow` (no command) |
| **Verify before deleting** an export/file | `fallow dead-code --trace <file>:<export>` |
| Auto-fix safe unused code | `fallow fix` |

Plus, for what fallow doesn't cover: `tsc --noEmit` (types), `npx eslint .` (`no-unused-vars`, `exhaustive-deps`, `no-explicit-any`, `no-console`), `react-doctor` (accessibility, bundle size). Measured hits → batch tier, still subject to the **brake** below (`fallow dupes` finds clone candidates; the rule of three decides).

## Coupling — frontend forms, worst → best

- **Cross-feature import** (content coupling — the cardinal sin) — `@features/<other>` from inside another feature. Forbidden by the vertical-slice rule; `fallow` flags it as a boundary violation. Fix: extract to `shared/` (if 2+ consumers) or duplicate (if not).
- **Shared mutable store reached everywhere** (common coupling) — a Zustand store imported across unrelated features.
- **Prop drilling** (stamp / control coupling) — a prop threaded through 3+ layers that don't use it. Fix: composition, context (sparingly), or a hook.
- **Context-provider over-coupling** — a context whose every change re-renders a wide subtree.
- **A small, intentional prop interface** (the goal).

## Cohesion — why a component or feature sprawls

- **God component** — a `.tsx` doing fetching + state + business logic + layout. Fix: business/stateful logic → a `use*` hook (colocated in the feature's `hooks/`), keep the component presentational [`rn-component-patterns.md`].
- **Logic / raw fetch in a component** — fetching belongs in TanStack Query hooks, not components [`rn-component-patterns.md`]. Misplaced logic = low cohesion.
- **Feature folder of unrelated screens** — logical, not functional, cohesion.

## Tests — the probes that surface findings

- **Deletion test** (`/codebase-design`) — delete the component / hook / export: complexity _vanishes_ → dead/duplicate/shallow; _ripples_ → coupled. `fallow dead-code --trace` makes this exact for exports.
- **Prop-count test** — a sprawling prop interface means the component is shallow or doing too much (the frontend's "wide interface").
- **Name-without-"and"** — a component or hook you can't name without "and" does more than one thing.
- **Shared-extraction test** — used by 2+ features → belongs in `shared/`; used by one → keep it colocated.

## The brake — every card carries the counter-argument

Relentless is not reckless. Before proposing an extraction / merge / delete, state why you might _not_:

- **Colocation > premature shared** — move to `shared/` only at the **second** consumer [`rn-component-patterns.md`]. One consumer = keep it in the feature.
- **Some prop drilling is fine** — 1–2 levels is cheaper than a context, which carries a re-render cost. Don't reach for context prematurely.
- **Duplication > wrong abstraction** — two components that look alike today but evolve differently stay separate [vault: wiki/concepts/DRY Principle.md].
- **A presentational wrapper can earn its place** — a thin component that centralizes a token or style decision isn't automatically shallow.
