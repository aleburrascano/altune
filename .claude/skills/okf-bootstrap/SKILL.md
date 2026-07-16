---
name: okf-bootstrap
description: Generates an initial okf/ knowledge bundle from a cold codebase — deterministic scan, human-approved concept list, blind-judged drafts. Use when the user asks to bootstrap, seed, or generate okf/ for a repo, or when okf/ is empty or near-empty.
---

# OKF Bootstrap

## Overview

Bulk-generates an initial okf/ bundle across an existing codebase. Unlike
okf-staleness-fix, which only ever fixes one small diff at a time, bootstrap
has to decide what the concepts *are* across the whole codebase from a cold
start — one-time and human-supervised.

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

Copy this checklist and check off items as you complete them:

```
Bootstrap Progress:
- [ ] 1. Scan the repo file list
- [ ] 2. Check existing okf/ coverage
- [ ] 3. Propose candidates (write nothing)
- [ ] 4. Human approval
- [ ] 5. Draft concepts (writer)
- [ ] 6. Verify drafts (blind judge)
- [ ] 7. Finalize (stamp, write, git add)
- [ ] 8. Report
```

### 1. Scan

From the repo root, build the candidate pool deterministically:

```bash
git ls-files --cached --others --exclude-standard | grep -v '^okf/'
```

Every tracked and untracked-but-not-gitignored file, excluding `okf/`
itself. This is the deterministic part of this skill, so scope never
silently drifts between runs.

### 2. Check existing coverage

If `okf/` already has concepts (re-running bootstrap on a partially-seeded
repo), read the `index.md` files and grep the `resource:` frontmatter
fields across `okf/` (`grep -rn "^resource:" okf/`) to see what's already
documented. Don't propose a concept for a resource that's already covered.

### 3. Propose candidates (dry run — write nothing yet)

Group the scanned files into concepts using this granularity rule: one
concept per named unit a person would naturally ask "what is X" about — a
database table, an API endpoint group, an exported service/module, a
cross-cutting playbook (e.g. an auth flow, an incident-response process).
Not every concept needs a `resource`: abstract or cross-cutting concepts may
have none.

Present the full candidate list to the user before writing anything, one
line per concept:

```
1. [type] title — resource (or "no resource — cross-cutting") — one-line description
2. ...
```

### 4. Get human approval

Wait for the user's response. They may approve all, approve a subset, ask
you to re-group, rename, split, or merge candidates, or reject the list
outright and ask for a different cut. Re-run step 3 with their feedback if
they ask for changes. No concept file exists until this step completes.

### 5. Generate (writer step, per approved candidate)

For each approved candidate, read its `resource` file(s) (if any) and draft
the concept file:

```yaml
---
type: <concept type, e.g. "API Endpoint">
title: <short identifier>
description: <one sentence>
resource: <path or path/ prefix, omit entirely if none>
tags: [<relevant tags>]
---

<body: what it is, how it works, anything a newcomer would need>
```

No `verified_commit` yet — stamped at step 7.

### 6. Judge (per candidate)

Blind judge with two inputs — the current resource content and the draft —
and this question:

> Does this draft accurately and substantively describe the current
> resource? This is a fresh description, not a diff review. Answer APPROVE,
> or REJECT with the specific inaccuracies or omissions.

Two-strike rule: a struck-out candidate is dropped from the generated set
and noted in the final report for manual attention — it never blocks the
rest of the batch.

### 7. Finalize

For every judge-approved draft:
- Stamp `verified_commit`.
- Write the file to `okf/<path>.md`, matching the folder organization
  already used elsewhere in this repo's `okf/` (or a sensible new
  subdirectory grouped by domain, e.g. `okf/api/`, `okf/data/`, if this is
  the very first concept in that domain).
- Add or update the directory's `index.md` entry for it.
- `git add` the new file.

### 8. Report

Summarize: how many concepts were generated, how many struck out (and why),
and where the new files landed. Leave the commit to the user — this skill
stages files, it doesn't commit them.

## Red Flags

- Any concept file written before step 4 approval completes.
- `verified_commit` appearing anywhere but step 7.
- A concept proposed for a resource step 2's grep already shows as covered.
- A third judge attempt.
- Running `git commit` — the user commits.
