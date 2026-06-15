---
name: update-docs-freshness
description: |
  Auto-fires from post-tool-commit-doc-drift hook when a commit touched code without touching expected
  doc artifacts. ALSO fires explicitly when user says "update the docs", "doc drift", "sync docs".
  Reads the flagged artifact + the changed code, proposes minimal doc edit, asks for approval, commits.
when_to_use: |
  Use when drift detector flags. Use after substantial code changes when you sense docs are behind.
---

# Update docs freshness

## What this skill does

Restores synchronization between code state and doc state. **Minimal** edits — surgical changes, never wholesale rewrites.

## Trigger contexts

### Auto-fired from drift detector

Hook input includes:
- `changed_files: [...]` — files touched in the most recent commit
- `flagged_docs: [...]` — docs the detector thinks are out of sync
- `reason: "..."` — why the detector flagged

### Explicit invocation

User says "update the docs" or names a specific doc.

## The loop

For each flagged doc:

1. **Read the doc.** Understand what it claims.
2. **Read the changed code.** Understand what changed.
3. **Locate the divergence.** Specific section, sentence, code example, list entry.
4. **Propose minimal edit.** Show diff. Ask user to approve.
5. **On approval:** apply edit. Commit with scope matching the doc (`docs(adr): refresh 0007 for new auth flow` or `docs(spec/library): update acceptance criterion 3 to match impl`).
6. **If the edit reveals deeper drift** (the doc is fundamentally out of date, not just a small lag): stop, flag to user, suggest writing a new ADR or `/audit-docs` sweep.

## When the code is wrong (not the docs)

If the code drifted from a spec/ADR that's still the source of truth: don't edit the doc. Instead:
- Stop and surface the discrepancy.
- Suggest the user either revise the spec/ADR (with `/adr-write` superseding the old one) OR fix the code to match.

The doc is the **source of truth** when it's an ADR, spec, or architectural doc. The code is the source of truth when it's about implementation details a doc shouldn't have been claiming.

## When the doc isn't actually behind

False positives from the detector happen. If after reading both, the doc is fine:
- Note this in the commit (`docs(<scope>): confirm doc still accurate after refactor`).
- Suggest tuning the drift detector if the false positive is repeated.

## Anti-patterns

- Rewriting a whole doc when only one line is stale. Surgical edits.
- Updating docs without reading the code first.
- Auto-committing without user approval (this skill always asks).
- Silently squashing the doc into matching code that's actually wrong.
