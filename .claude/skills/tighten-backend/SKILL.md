---
name: tighten-backend
description: >
  Relentless whole-backend structural review — finds shallow modules, needless
  coupling, duplication, and dead code across every Go bounded context, then
  triages fixes by blast radius. For frontend or language-agnostic structural
  work, use /improve-codebase-architecture.
disable-model-invocation: true
argument-hint: "[context-name]   # default: all contexts"
---

# Tighten Backend

Relentless structural review of the Go backend. The aim: every **module** _tight_ — a small **interface**, few dependencies, no dead weight — without severing coupling that's actually load-bearing. Where `/improve-codebase-architecture` surfaces a few candidates and stops at a menu, this holds nothing back: every context, every shallow seam, every duplicated or dead path.

Run `/codebase-design` first for the vocabulary (**module, interface, depth, seam, leverage, locality**) and the **deletion test** — use those terms exactly, never restate them. The coupling/cohesion taxonomies and the structural tests this skill adds live in [HEURISTICS.md](HEURISTICS.md).

## Process

### 1. Fan out — one Explore agent per context

Dispatch one read-only `Explore` agent per bounded context under `services/go-api/internal/` (skip `app/`; an argument narrows to a single context). Each agent reads [HEURISTICS.md](HEURISTICS.md) and returns a findings inventory — every shallow module, needless coupling, duplication, and dead path it finds, _relentless_, nothing held back. **Name the move, don't just name the smell:** every finding must cite the specific refactoring technique that fixes it from `.claude/rules/refactoring/` (the Fowler catalog, adapted to Go) — "this is _Inline Class_ — fold it into its caller," not "this is shallow." A finding that can't name a technique is a vibe; drop it or sharpen it. Every finding carries a **blast radius**: the callers that ripple if it changes, verified by reference search across packages — never eyeballed from one file. Cross-package usage is invisible to a single read; a function that looks dead in `app.go` may be called from `search_wiring.go`.

Completion: every context has a returned inventory, every finding a verified blast radius. No context skipped.

### 2. Report

Write the self-contained HTML report per [REPORT.md](REPORT.md) to the OS temp dir and open it. Two parts:

- A whole-backend **coupling graph** (Mermaid): inter-context dependencies and any cycles — the answer to _"can this context be pulled out?"_ A cycle means it can't.
- One **self-grilled card** per finding: problem · proposed shape · verified blast radius · surviving tests · the **counter-argument** (why you might _not_ — the brake against over-decoupling). The grilling is done in the card; you read finished reasoning, not a live interview.

### 3. Triage — confidence × blast radius

Walk every finding; none is silently dropped. Sort into three tiers:

- **Batch-approve** — high confidence, low blast (dead code, local duplication, trivial decomposition). Approve as a group, no discussion.
- **Skim-confirm** — judgment calls; the card already argues both sides. One sentence each.
- **Grill live** — structural, high blast (extract a context, collapse adapters behind a facade, reshape the composition root). Only these get real back-and-forth.

### 4. Fix the approved

Report-only by default. Fix only what's approved, highest blast first, commit per context. A rejection with a load-bearing reason → offer an ADR so the next run doesn't re-surface it.
