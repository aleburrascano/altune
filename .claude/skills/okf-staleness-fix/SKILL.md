---
name: okf-staleness-fix
description: Use when a git commit is blocked by the OKF pre-commit hook because a resource file changed without its matching okf/ concept being updated
---

# OKF Staleness Fix

## Overview

The OKF pre-commit hook blocks commits when a staged file matches a
concept's `resource` field but the concept file itself wasn't staged. This
skill resolves that block: draft the concept update (writer), verify it's
accurate (judge), then finalize and commit.

## When to Use

Triggered by a blocked commit. The hook's stderr names one or more
mismatched `<resource, concept>` pairs, one per line, in the form:

```
<resource-path> changed but <concept-path> was not updated
```

## Process

For **each** mismatched pair reported by the hook:

1. **Gather inputs**
   - Concept's current content: `Read <concept-path>`
   - The diff: `git diff --cached -- <resource-path>`
   - Rename check: `git status --porcelain --find-renames -- <resource-path>`

2. **Writer step** — draft the update
   - Update only the body (Schema/Examples/whatever the diff affects) to
     accurately reflect the change.
   - Leave every frontmatter field untouched **except**:
     - Renames: update `resource:` to the new path.
     - Deletions: don't delete the concept. Add `status: deprecated` and a
       short note in the body referencing the removal.
   - Never set `verified_commit` here — that's step 4, not the writer's job.

3. **Judge step** — verify the draft
   - Give the judge the diff, the concept's original content, and the
     writer's draft. Ask: is this an accurate, substantive reflection of the
     change — not a rubber stamp?
   - If rejected: redraft once (2 attempts total).
   - If still rejected: **stop**. Leave the commit blocked. Surface the diff
     and the judge's rejection reason to the user for manual attention. Do
     not proceed to step 4 for this pair.

4. **Finalize** (only after judge approval)
   - Set `verified_commit` yourself via `git rev-parse HEAD` (the parent
     commit — deterministic, not a model judgment call).
   - Write the approved draft to the concept file.
   - Optionally append a dated entry to `okf/log.md` noting what was fixed.

After all pairs are resolved (or explicitly left blocked per step 3):
- `git add` every concept file you updated.
- Recommit (reuse the original commit message, or hand control back to the
  user).

## Red Flags

- Setting `verified_commit` inside the writer or judge step — it belongs to
  step 4 only, computed from `git rev-parse HEAD`, never guessed by a model.
- Retrying the judge more than once — 2 attempts total, then stop and
  surface for manual attention.
- Deleting a concept file because its resource was deleted — mark
  `status: deprecated` instead.
