---
name: okf-bootstrap
description: Use when the user asks to bootstrap, generate, or seed an initial okf/ knowledge bundle for this codebase from scratch
---

# OKF Bootstrap

## Overview

Bulk-generates an initial okf/ knowledge bundle across an existing codebase.
This is a one-time, human-supervised process — unlike the ongoing
writer/judge flow (see the okf-staleness-fix skill), which only ever fixes
one small diff at a time, bootstrap has to decide what the concepts *are*
across the whole codebase from a cold start.

## When to Use

The user explicitly asks to bootstrap, seed, or generate an initial okf/
bundle (e.g. "bootstrap okf for this repo", "generate the knowledge base").
Not triggered automatically by anything — there is no hook or CLI flag that
invokes this on its own.

## Process

### 1. Scan

Run `waymark bootstrap` via Bash from the repo root. It prints a JSON array
of every tracked and untracked-but-not-gitignored file, excluding `okf/`,
`node_modules/`, and `dist/`. This is the candidate pool — the deterministic
part of this skill, so scope never silently drifts between runs.

### 2. Check existing coverage

If `okf/` already has concepts (re-running bootstrap on a partially-seeded
repo), call `list_concepts()` via the waymark MCP server to see what's
already documented, and `find_concept_by_resource(file)` for files you're
unsure about. Don't propose a concept for a resource that's already covered.
If the MCP server is unavailable, fall back to `Read`/`Grep` over `okf/`
directly and say so explicitly.

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
outright and ask for a different cut. Do not generate any concept file
before this step completes. Re-run step 3 with their feedback if they ask
for changes.

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

Do not set `verified_commit` yet — that's step 7.

### 6. Judge (verify, per candidate)

For each drafted concept, have a separate judge subagent check: does this
accurately and substantively describe the current resource (not "does it
reflect a diff" — there is no diff here, this is a fresh description)? Give
the judge the resource file's current content and the draft.

- Rejected: redraft once (2 attempts total).
- Rejected again: drop this candidate from the generated set and note it in
  the final summary for manual attention. Do not block the rest of the
  batch on one failing candidate.

### 7. Finalize

For every judge-approved draft:
- Set `verified_commit` yourself via `git rev-parse HEAD` — deterministic,
  never guessed by a model, same rule as the ongoing writer/judge flow.
- Write the file to `okf/<path>.md`, matching the folder organization
  already used elsewhere in this repo's `okf/` (or a sensible new
  subdirectory grouped by domain, e.g. `okf/api/`, `okf/data/`, if this is
  the very first concept in that domain).
- `git add` the new file.

### 8. Report

Summarize: how many concepts were generated, how many were dropped after a
failed second judge pass (and why), and where the new files landed. Leave
the commit to the user — stage the files with `git add` but do not run
`git commit` yourself.

## Red Flags

- Writing any concept file before the human approval step (step 4)
  completes — the whole point of the dry run is nothing is written until
  approved.
- Setting `verified_commit` during the writer or judge step — it belongs to
  step 7 only, computed from `git rev-parse HEAD`.
- Proposing a concept for a resource `find_concept_by_resource` or
  `list_concepts` already covers.
- Retrying the judge more than once per candidate — 2 attempts total, then
  drop and report, don't block the batch.
- Committing on the user's behalf — this skill stages files, it doesn't
  commit them.
