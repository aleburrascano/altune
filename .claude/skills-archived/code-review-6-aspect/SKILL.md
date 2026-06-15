---
name: code-review-6-aspect
description: |
  Fires when the user says "review this", "pre-merge review", "code review", "sign off", or when a feature's
  /verify-end-to-end is green and ready for merge. Dispatches 6 specialist subagents in parallel —
  architecture, security, performance, testing, quality, docs — each reviewing the diff against its single
  concern. Merges findings into a single review report with severity-grouped action items.
when_to_use: |
  Use before merge / before declaring a feature done. Skip for trivial changes (typo, doc-only).
---

# 6-aspect parallel code review

## Mandatory first step

Each dispatched subagent queries the software-architecture-design vault MCP at start. Aggregating logic here:
1. `mcp__claude-code__vk_search "code review"` — for any framework-specific review wisdom (e.g., review checklists, common Python/TS pitfalls).
2. Pass the diff (or PR description) to each subagent as context.

## What this skill does

1. **Determine review scope.** Either:
   - A specific commit range (`git log <base>..HEAD`)
   - Uncommitted changes (`git diff`)
   - A specific feature folder (`docs/specs/<feat>/` + corresponding code paths)
   Confirm scope with user before dispatching.

2. **Dispatch 6 subagents IN PARALLEL** (single message, multiple Agent calls):
   - `architecture-reviewer` — pattern compliance, layer boundaries, hexagonal discipline, vault-pattern alignment
   - `security-reviewer` — input validation, auth, secrets, OWASP risks, prompt injection on user-provided content
   - `perf-reviewer` — N+1, hot paths, async correctness, bundle size on mobile, render perf
   - `testing-reviewer` — test coverage gaps, weak assertions, sacred-tests violations, missing edge cases
   - `quality-reviewer` — SRP/DRY/KISS/YAGNI/Law of Demeter; naming; cohesion; readability
   - `docs-reviewer` — spec/ADR/glossary alignment, nested CLAUDE.md freshness, code comments, AIDEV anchor preservation

3. **Wait for all 6.** No partial reports.

4. **Merge + dedupe findings.** Multiple reviewers flagging the same thing → consolidate. Group by severity:
   - **🚨 Block** — must fix before merge (broken invariants, security holes, sacred-test edits without confirmation, layer violations)
   - **⚠️ Should fix** — quality issues with measurable cost (perf concerns, missing tests, unclear naming)
   - **💡 Consider** — suggestions, alternatives, learning opportunities

5. **Report** with this structure:

   ```markdown
   # Code review — <scope>

   ## 🚨 Block (N items)
   - [security] <file:line> — <issue> — fix: <suggested action>
   - …

   ## ⚠️ Should fix (M items)
   - …

   ## 💡 Consider (K items)
   - …

   ## Aspect summary
   - architecture: <one-line>
   - security: <one-line>
   - …

   ## Recommendation
   <merge-ready / fix-and-rerun / spec-change-needed>
   ```

6. **If "fix-and-rerun":** user addresses, then re-run this skill on the new diff.

## Anti-patterns

- Single reviewer trying to cover all aspects — concern-trading happens silently.
- Reviews without the vault lookup — risks becoming "Claude's opinion" rather than industry-pattern-grounded.
- Treating 💡 items as required — they're suggestions, not blockers.
- Skipping the consolidation step — duplicate findings drown the signal.
