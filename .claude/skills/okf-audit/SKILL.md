---
name: okf-audit
description: Audits an existing okf/ bundle against the codebase — bundled lint for mechanical findings, judgment scan for drift, coverage gaps, misfiled or wrong-granularity concepts; executes only user-approved fixes. Use when the user asks to audit, re-verify, restructure, or rework the okf/ bundle. Not for a blocked commit (okf-staleness-fix) or an empty bundle (okf-bootstrap).
---

# OKF Audit

## Overview

The periodic middle ground between the two sibling skills: okf-bootstrap
creates the bundle cold; okf-staleness-fix repairs one hook-blocked diff.
This skill re-audits an existing, approved bundle against the codebase —
drift the hook never saw (commits made while it was broken or bypassed with
--no-verify), coverage gaps, and structural problems (misfiled concepts,
wrong granularity).

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
Audit Progress:
- [ ] 1. Mechanical scan (okf-lint)
- [ ] 2. Judgment scan (coverage, filing, granularity)
- [ ] 3. Propose findings (write nothing)
- [ ] 4. Human approval
- [ ] 5. Execute approved findings
- [ ] 6. Finalize (stamp, re-lint, git add)
- [ ] 7. Report
```

### 1. Mechanical scan

Run the lint bundled with this skill, from the repo root:

```bash
python .claude/skills/okf-audit/scripts/okf-lint.py
```

It reports:
- **ERROR** — broken links, bad frontmatter, duplicate slugs
- **STALE** — resource files changed since the concept's `verified_commit`
- **WARN** — index-sync gaps, orphans, unknown `verified_commit`

### 2. Judgment scan

Three questions the lint can't answer:

- **Coverage**: enumerate the codebase's named units — backend modules under
  `services/go-api/internal/`, mobile features/shared subsystems, DB tables
  via `services/go-api/migrations/`, CI workflows — and diff that list
  against the concepts' `resource:` fields. Every unit a person would ask
  "what is X" about should have a concept.
- **Filing**: does each concept's bundle path match what its resource
  actually is? (Precedent: `admin-mission-control.md` sat under
  `backend/discovery/` while its resource was `internal/admin/` — moved.)
- **Granularity**: split concepts that describe several independent
  machines; merge trivia that can't stand alone. A concept should answer
  one "what is X".

For heavy code-reading (does this 18K-LOC module match its doc?), dispatch
Explore subagents rather than reading inline.

### 3. Propose (dry run — write nothing yet)

Present one findings list, grouped by action, one line each:

```
RE-VERIFY  okf/backend/catalog.md — 31 files drifted since 6a047a0
MOVE       okf/backend/discovery/foo.md → okf/backend/foo.md — resource is internal/foo/
SPLIT      okf/backend/big.md → big-a.md + big-b.md — two unrelated machines
ADD        okf/backend/newmod.md — module has no concept
FIX        broken link / index entry / frontmatter field
```

### 4. Get human approval

Wait. The user may approve all, a subset, or re-cut the list. Nothing is
written before this completes.

### 5. Execute (per approved finding)

- **RE-VERIFY** — writer against the cumulative diff:
  `git diff <verified_commit>..HEAD -- <resources>`. Writer updates the
  body to match reality; if the diff turns out to be cosmetic (formatting,
  comments, tests only), the writer may leave the body unchanged — the
  stamp in step 6 is still the point. Then blind judge with three inputs —
  the cumulative diff, the concept's original content, and the writer's
  draft — and this question:

  > Is this draft an accurate, substantive reflection of the current code —
  > not a rubber stamp? Answer APPROVE, or REJECT with the specific
  > inaccuracies or omissions.

  Two-strike rule: a struck-out concept is left untouched and reported.
- **MOVE** — `git mv`, then repoint every inbound link (grep the old
  slug/path across `okf/`) and both affected `index.md` files.
- **SPLIT / MERGE** — write the new concept(s), repoint links, update
  indexes, delete or deprecate the old file (deletion is fine here because
  the *resource* still exists; `status: deprecated` is for concepts whose
  resource was removed).
- **ADD** — for each new concept: read its resource file(s), draft the
  concept using the frontmatter schema above, then blind judge with two
  inputs — the resource content and the draft — asking whether the draft
  accurately and substantively describes the current resource (a fresh
  description, not a diff review). Two-strike rule.

### 6. Finalize

- Stamp `verified_commit` on every touched concept.
- Re-run the lint from step 1 and iterate until 0 errors and every
  approved STALE item is cleared.
- `git add` the changed files. Leave the commit to the user.

### 7. Report

Findings acted on, findings dropped (struck out or user-declined) and why,
and the final lint tally.

## Red Flags

- Anything written before step 4 approval completes.
- `verified_commit` appearing anywhere but step 6.
- A stale concept's `verified_commit` bumped without a writer/judge pass —
  that's laundering staleness, not fixing it.
- A third judge attempt.
- Regenerating concepts that only needed a re-verify — this skill edits the
  bundle it has; okf-bootstrap generates from scratch.
- Running `git commit` — the user commits.
