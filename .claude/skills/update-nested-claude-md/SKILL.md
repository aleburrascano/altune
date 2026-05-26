---
name: update-nested-claude-md
description: |
  Auto-fires after every 3rd commit affecting a feature folder. Regenerates the AUTO-MAINTAINED block
  in the feature's nested CLAUDE.md (gotchas, key terms, patterns) from current code. The user's
  hand-written content above/below the AUTO-MAINTAINED markers is preserved. Auto-commits.
when_to_use: |
  Auto-only. Manual invocation: "regenerate nested CLAUDE.md for <feature>".
---

# Update nested CLAUDE.md

## What this skill does

For a specified feature folder (e.g., `apps/mobile/src/features/library/` or `services/api/src/altune/domain/catalog/`):

1. **Locate the nested CLAUDE.md.** If none, create from `apps/mobile/src/features/_template/CLAUDE.md`.
2. **Read all files in the folder.**
3. **Regenerate the AUTO-MAINTAINED block** between the markers:
   ```
   <!-- AUTO-MAINTAINED:BEGIN -->
   <!-- AUTO-MAINTAINED:END -->
   ```
4. **Preserve everything outside the markers** verbatim.
5. **Commit:** `docs(claude-md): regenerate <feature> auto-maintained block`.

## What goes in the AUTO-MAINTAINED block

Auto-derived from the code, ≤30 lines total:

```markdown
<!-- AUTO-MAINTAINED:BEGIN -->
## Auto-maintained

### Files (key)
- `<file>` — `<one-line role>`
- ...

### Public API surface (this feature exposes)
- `<symbol>` — `<one-line purpose>`

### Dependencies on other features / shared
- `shared/api-client` — for `<purpose>`
- `features/<other>` — NONE (vertical slice rule preserved)

### Test files
- `<test file>` — covers `<what>`

<!-- AUTO-MAINTAINED:END -->
```

## What stays outside (hand-written)

Above the BEGIN marker:
- Feature title + 1–2 sentence summary
- Hand-curated "Key terms" (terms that mean something specific in *this* feature)
- "Patterns specific here" — things you decided this feature does differently
- "Known gotchas" (auto-grown via `/compound-learning` but user-edited)

## When to skip the regeneration

- If the folder has <3 files, the block is noise. Don't generate.
- If a previous regeneration is < 1 hour old (avoid thrashing on rapid commits).

## Rollback

The commit is isolated — just this one file. `git revert <commit>` rolls it back cleanly. If the regeneration looks wrong:
1. Revert the commit.
2. Open an issue / capture in `/compound-learning` describing the failure mode (so the skill can be improved).

## Anti-patterns

- Touching content outside the AUTO-MAINTAINED markers.
- Generating an empty block (skip if nothing meaningful to say).
- Regenerating CLAUDE.md files that aren't in feature/context folders (root and layer-global CLAUDE.md are hand-maintained).
