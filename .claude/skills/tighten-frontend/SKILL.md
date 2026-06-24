---
name: tighten-frontend
description: >
  Relentless whole-frontend structural review — finds shallow components, prop
  drilling, cross-feature coupling, god components, duplication, and dead code
  across every vertical-slice feature, then triages fixes by blast radius. For
  backend structural work use /tighten-backend; for agnostic module depth,
  /improve-codebase-architecture.
disable-model-invocation: true
argument-hint: "[feature-name]   # default: all features"
---

# Tighten Frontend

Relentless structural review of the Expo / React Native frontend. The aim: every **module** — feature, component, hook — _tight_ — a small **interface** (few props), few dependencies, no dead weight — without over-extracting to `shared/` before a second consumer earns it. Where `/improve-codebase-architecture` surfaces a few candidates and stops at a menu, this holds nothing back: every feature, every shallow component, every cross-feature import, every duplicated or dead path.

Run `/codebase-design` first for the vocabulary (**module, interface, depth, seam, leverage, locality**) and the **deletion test** — use those terms exactly. A component's prop list _is_ its interface; a hook's signature _is_ its interface. The frontend coupling/cohesion smells and the tooling this skill adds live in [HEURISTICS.md](HEURISTICS.md).

Two kinds of finding, and the difference is load-bearing. **Measured** — dead code, duplication, complexity, circular deps, cross-feature imports — is exact: `fallow` computes it, never eyeball it. **Judgment** — god components, prop drilling, wrong shape — needs the agent's eye. The skill fails when it _samples_ a judgment call or _eyeballs_ a measured one. Cover, don't sample.

## Process

### 1. Measure — deterministic pass (main thread)

Run `fallow` from `apps/mobile/` — one Rust-native pass covering **unused code, duplication, complexity, circular dependencies, and architecture-boundary violations** (the last is your forbidden cross-feature import). Add `tsc --noEmit`, ESLint, and `react-doctor` (a11y, bundle) for what fallow doesn't cover. Commands + the verify-before-delete trace are in [HEURISTICS.md](HEURISTICS.md). The output is exact and complete — these findings go straight to the batch tier, no grilling.

### 2. Fan out — one Explore agent per feature

Dispatch one read-only `Explore` agent per vertical slice under `apps/mobile/src/features/` (plus `shared/` and `app/` routes; an argument narrows to one). Each reads [HEURISTICS.md](HEURISTICS.md) for the **judgment** axes — god components, prop drilling, business logic that belongs in a hook, shallow wrapper components, a sprawling prop interface, premature-or-missing `shared/` extraction. **Name the move, don't just name the smell:** every judgment finding must cite the specific refactoring technique that fixes it from `.claude/rules/refactoring/` (the Fowler catalog, adapted to RN/TS) — "business logic in the component → _Extract Method_ into a feature hook," not "this component is doing too much." A finding that can't name a technique is a vibe; drop it or sharpen it. **Cover, don't sample:** enumerate every file in the feature, report the count, read each _in full_ — `Explore` defaults to locating, so the instruction is explicit. _Relentless_, nothing held back. Every finding carries a verified **blast radius**: who renders this component or calls this hook — `fallow dead-code --trace <file>:<export>` for exports, reference search for the rest. Never eyeballed.

Completion: every file in every feature accounted for, none skipped; every finding a verified blast radius.

### 3. Report

Write the self-contained HTML report per [REPORT.md](REPORT.md) to the OS temp dir and open it. Two parts:

- An inter-feature **coupling graph** (Mermaid) from `fallow`'s boundary + cycle output: features (+ `shared/`) as nodes, imports as edges. Cross-feature edges and cycles are red — each is a vertical-slice violation, the answer to _"can this feature be lifted cleanly?"_
- One **self-grilled card** per finding: problem · proposed shape · verified blast radius · surviving tests · the **counter-argument** (why you might _not_ — the brake against over-extracting). The grilling is in the card; you read finished reasoning, not a live interview.

### 4. Triage — confidence × blast radius

Walk every finding; none is silently dropped. Sort into three tiers:

- **Batch-approve** — measured findings (fallow/eslint/tsc) and high-confidence/low-blast ones (dead code, duplication, a stray cross-feature import). Approve as a group.
- **Skim-confirm** — judgment calls; the card already argues both sides. One sentence each.
- **Grill live** — structural, high blast (split a god screen, lift shared state, collapse a prop-drilled tree behind context). Only these get real back-and-forth.

### 5. Fix, then loop until dry

Report-only by default. Fix only what's approved, highest blast first; `fallow fix` auto-fixes safe unused code. Then **re-run `fallow` and re-scan the touched features until a pass finds nothing new** — fixes expose fresh findings: delete a component and its only-child hook dies; split a god screen and new prop coupling surfaces. Commit per feature. A rejection with a load-bearing reason → offer an ADR so the next run doesn't re-surface it.
