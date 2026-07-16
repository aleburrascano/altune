# OKF Kit — Requirements

**Date:** 2026-07-16
**Status:** Confirmed (v1 scope cut to "bundle as-is")

## Summary

Package the existing OKF tooling (3 skills + 2 scripts) as a plain git repo — a skills-and-tools kit friends can clone and wire into their own repos — deliberately NOT a Claude Code plugin. v1 ships the current files verbatim with a README install runbook; professionalization is deferred.

## Problem Frame

OKF (knowledge bundle + lifecycle skills + staleness hook) currently lives inside altune. A friend has seen it and wants it; more interest is anticipated; the owner also wants zero-effort setup on future projects. The tooling assumes altune-specific plumbing (husky wiring, `scripts/` paths, conventions living in repo structure rather than written down).

## Requirements

- R1. A standalone git repo (`okf-kit`, sibling of altune) containing, verbatim: `skills/okf-bootstrap`, `skills/okf-audit`, `skills/okf-staleness-fix`, `scripts/okf-lint.py`, `scripts/okf-staleness-check.sh`.
- R2. A README that (a) explains what OKF is (one screen, links Google's OKF v0.1 spec), (b) documents the manual install runbook: copy skills into the target repo's `.claude/skills/`, copy scripts into `scripts/`, wire the staleness check into the repo's pre-commit path (husky / lefthook / bare `.git/hooks` variants), add the knowledge-base section to the target CLAUDE.md, (c) states the lifecycle: bootstrap → staleness-fix (per commit) → audit (periodic).
- R3. Kit is the canonical home going forward; altune's local copies remain for now (migration deferred).
- R4. Local repo only in v1 — pushing to GitHub is the owner's manual step.

## Key Decisions

- **Kit, not plugin** — plugin system judged too buggy to depend on; a plain repo with a documented install path delivers the same value. Revisitable; layout wouldn't change much.
- **Keep 3 skills; never split finer** — triggers are mutually exclusive (empty bundle / blocked commit / periodic drift); finer skills would degrade routing.
- **Scripts are vendored into consuming repos** — git hooks and CI must work without any kit/plugin present.
- **Ship as-is, improve later** — v1 is a packaging exercise, zero skill rewrites.

## Scope Boundaries

**Deferred for later (v2 candidates, rough priority order):**
1. `okf-setup` skill — automated installer/wirer replacing the README runbook (highest-risk surface: foreign hook managers, existing CLAUDE.mds).
2. Best-practices pass — extract shared `references/conventions.md` (portable OKF house spec: `verified_commit` contract, index discipline, router pattern) + `references/writer-judge.md`; trim SKILL.mds to point at them.
3. Eval fixture repo with planted defects; 3 scenarios per skill (bootstrap has never run in its de-waymarked form — test before wide sharing).
4. altune migrates to consume the kit (delete local copies).
5. Document audit's token cost for small-plan users.

**Outside this product's identity:**
- Claude Code plugin/marketplace distribution.
- Finer-grained skills (add-concept, reverify-one, …).
- A Claude Code lifecycle hook duplicating the git hook.
- Non-Claude-Code agent support (Codex/Gemini).
- Shipping any okf/ content — bundles are always per-repo output.

## Assumptions / Known Risks

- okf-bootstrap's de-waymarked flow is untested end-to-end (edited 2026-07-16, never executed). First friend install should treat bootstrap output with the human-approval gate it already mandates.
- The staleness hook only guards the resource→concept direction; born-wrong concepts are caught only by audit's judge layer. The README should say "run the audit occasionally."
- Kit and altune copies are duplicated until migration (deferred item 4) — drift risk accepted for v1.
