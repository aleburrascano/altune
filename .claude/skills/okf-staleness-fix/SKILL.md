---
name: okf-staleness-fix
description: Repairs a commit blocked by the OKF pre-commit hook — writer updates each out-of-date okf/ concept, a blind judge verifies it, then recommit. Use when a git commit fails with "<resource> changed but <concept> was not updated".
---

# OKF Staleness Fix

## Overview

The OKF pre-commit hook blocks commits when a staged file matches a
concept's `resource` field but the concept file itself wasn't staged. This
skill resolves that block: draft the concept update (writer), verify it
(blind judge), then finalize and recommit. The hook's stderr names the
mismatched `<resource, concept>` pairs, one per line:

```
<resource-path> changed but <concept-path> was not updated
```

## Shared conventions

All three okf skills (bootstrap, staleness-fix, audit) follow these rules:

- **Frontmatter**: `type`, `title`, `description` required. `resource:` names
  the repo path(s) the concept documents — comma-separated allowed, trailing
  `/` = prefix match, omitted for cross-cutting concepts. `tags: [..]`.
  `verified_commit` = the commit the body was last verified against.
- **Blind judge**: every draft is verified by a separate judge subagent
  (Agent tool, general-purpose) given only the artifacts named in its step —
  never the writer's reasoning or this conversation's history.
- **Two-strike rule**: a rejected draft is redrafted once, using the judge's
  specifics. Rejected twice → stop, leave that item untouched, report it.
  Never a third attempt.
- **Stamped, not guessed**: `verified_commit` is set only at finalize,
  computed via `git rev-parse HEAD`. The writer and judge never touch it.
- Concepts record what code can't say — why, invariants, contracts,
  gotchas — never what a grep answers.

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

3. **Judge step**
   - Blind judge with three inputs — the diff, the concept's original
     content, and the writer's draft — and this question:

     > Is this draft an accurate, substantive reflection of the change —
     > not a rubber stamp? Answer APPROVE, or REJECT with the specific
     > inaccuracies or omissions.

   - Two-strike rule: struck out → leave the commit blocked, surface the
     diff and the judge's rejection reason to the user, and skip step 4 for
     this pair.

4. **Finalize** (only after judge approval)
   - Stamp `verified_commit` (here `git rev-parse HEAD` is the parent
     commit — the diff being described is still staged).
   - Write the approved draft to the concept file.
   - Optionally append a dated entry to `okf/log.md` noting what was fixed.

After all pairs are resolved (or explicitly left blocked per step 3):
- `git add` every concept file you updated.
- Recommit (reuse the original commit message, or hand control back to the
  user).

## Red Flags

- `verified_commit` appearing anywhere but step 4.
- A third judge attempt.
- A concept deleted because its resource was deleted — `status: deprecated`
  instead.
- A judge that saw the writer's reasoning.
