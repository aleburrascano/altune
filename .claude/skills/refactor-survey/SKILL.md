---
name: refactor-survey
description: Survey ONE module for behavior-preserving refactors and emit curated, banded type:refactor tickets. Read-only — tickets are the output, never code. Fans out a focused pass per structural category so each goes deep. Use per module when clearing structural debt before features. Trigger words: refactor-survey, survey a module for refactors, refactor tickets for <module>, refactor debt.
---

# Refactor survey

**Structure only — behavior stays identical.** Runs the shared survey engine with the refactor lens below. Output: `type:refactor` tickets.

**Follow [`.claude/survey-engine.md`](../../survey-engine.md)** with everything here.

## Ticket type & Done-when
- Type label `type:refactor`. Title prefix `[Refactor]:`.
- **Done-when** (the body template): gate green (behavior unchanged) · the concrete structure outcome · every new abstraction names what it beat in the PR (else delete it) · `codebase-workflow` run on the diff · nested `CLAUDE.md` updated if files moved.

## Lens — fan out ONE focused pass per category
Doctrine (the authoritative rubric each pass leans on): `~/.claude/workflow/pattern-budget.md`, the **Navigability** group in `~/.claude/workflow/robustness-domains.md`, and `~/.claude/workflow/code-style.md`.

1. **Duplication / DRY** — the same shape written 3+ times *and drifting*; count the copies. Two copies is not a finding (Rule of Three).
2. **Single responsibility (SRP)** — god objects/files/functions; line-count and dependency-count outliers; "many reasons to change".
3. **Boundaries (DIP / ISP)** — imports pointing the wrong way; a layer reaching what it must not; a fat interface that should be segregated.
4. **Over-abstraction (KISS / YAGNI)** — per `pattern-budget.md` + `style/over-correction.md`: an interface/generic/layer with **fewer than 2 real callers**, indirection with no second implementation, speculative "might need it" structure. (This is the *too-much-structure* direction; #2 is *too-little*.)
5. **Navigability** — Locality (a file holding more than one concept), Naming (not searchable / off the codebase's vocabulary), a missing seam map.

**Behavior-preserving only.** Anything that must change behaviour (validation, a bug, a perf fix) is a `harden` / `bug` ticket, not this.
