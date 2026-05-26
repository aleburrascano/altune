---
name: git-commit
description: |
  Fires when user says "commit", "commit this", "stage and commit", "ship this", "land this", "save my
  changes". Drafts a Conventional Commits message (type · scope · subject · body · footer), verifies it
  passes commitlint, ensures NO AI co-author lines (commit-msg hook strips them but don't generate
  them in the first place). Asks user to confirm before committing.
when_to_use: |
  Use when ready to commit. Use after /verify-end-to-end passes. Use after each TDD slice (separate
  test commit + impl commit).
---

# Git commit

## Mandatory pre-commit checks

1. **`git status`** — what's actually staged / unstaged. Confirm scope with user if unclear.
2. **`/verify-end-to-end`** must have passed recently (or run it now if not).
3. **No secrets** in the diff (`.env`, keys, tokens). The `pre-tool-file-guard` hook blocks these, but double-check.
4. **No `Co-Authored-By: Claude` or AI attribution** in any message you draft. (Hook strips, but don't generate.)

## Message format

Follow `.gitmessage` template. Validated by `commitlint.config.js`.

```
<type>(<scope>): <subject>

<body>

<footer>
```

| Field | Rules |
|---|---|
| `type` | One of: `feat`, `fix`, `refactor`, `chore`, `docs`, `test`, `perf`, `style`, `ci`, `revert` |
| `scope` | Required. From `commitlint.config.js` `scope-enum`. Feature scopes are added by `/feature-spec`. |
| `subject` | Imperative, lowercase, no trailing period, ≤72 chars total header |
| `body` | Explain *why*, not *what*. Reference spec/ADR/solution: `see docs/specs/library/spec.md`. Lines ≤100 chars. Blank line between body paragraphs. |
| `footer` | Optional. `Refs: #123, ADR-0007`. `BREAKING CHANGE: <description>` if applicable. |

## Examples

Good:
```
feat(library): add play count to track aggregate

The catalog spec (docs/specs/library/spec.md AC#3) requires play count
to be visible per track. Adding as an aggregate field maintains the
invariant that play count never decreases except via explicit reset.

Refs: ADR-0008
```

Bad:
```
update track.py
```

```
feat: stuff

Co-Authored-By: Claude
🤖 Generated with Claude Code
```

## Slice-aware commits

For TDD slices:
- Commit 1 (RED): `test(<scope>): add failing test for <behavior>`
- Commit 2 (GREEN): `feat(<scope>): <implementation summary>`
- Commit 3 (REFACTOR, optional): `refactor(<scope>): <what improved>`

Keeps bisect clean.

## When the commit is too big

If `git diff --stat` shows >10 files or >300 changed lines, ask: should this be 2+ commits?
- Slice into logical units.
- Stage selectively with `git add -p` if needed (interactive — work with user).

## After commit

1. Show `git log -1` output.
2. If the commit caused a doc drift (the post-tool-commit-doc-drift hook will flag), surface the warning.
3. If the commit-msg hook stripped attribution lines, mention it (so user knows the message was modified).

## Anti-patterns

- Multi-purpose commits ("fixed bug and refactored and updated docs").
- "WIP" commits that get pushed (use `git stash` or local branches).
- `git commit -a` without reviewing what's staged.
- Squashing TDD commits before merge — the test+impl separation is the bisect value.
