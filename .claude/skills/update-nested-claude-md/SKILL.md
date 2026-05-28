---
name: update-nested-claude-md
description: |
  Creates or refreshes nested CLAUDE.md for a feature folder OR a backend bounded-context layer.
  Auto-fires when the `stop-claude-md-hygiene` Stop hook flags a touched dir as missing CLAUDE.md
  or having a CLAUDE.md older than its source files. Legacy trigger: every 3rd commit affecting a
  feature folder (kept as a mid-feature refresh signal). Mobile feature dirs use an AUTO-MAINTAINED
  block (regenerated, hand-written sections preserved). Backend bounded-context dirs are fully
  hand-written — no AUTO-MAINTAINED block — generated from the dir's classes / Protocols / AIDEV
  anchors. Auto-commits.
when_to_use: |
  Fires automatically on session end via the `stop-claude-md-hygiene` Stop hook gate. Manual
  invocation: "regenerate nested CLAUDE.md for <dir>" — needed only to override the hook's <3-files
  skip rule or to refresh without dir changes.
---

# Update nested CLAUDE.md

## Which mode

The skill handles two dir shapes — pick by the path:

- **Mobile feature dir** — `apps/mobile/src/features/<feat>/` — uses an AUTO-MAINTAINED block (regenerable file/exports/deps/tests inventory) plus hand-written header. Template: `apps/mobile/src/features/_template/CLAUDE.md`.
- **Backend bounded-context dir** — `services/api/src/altune/{domain,application,adapters/inbound/http,adapters/outbound,adapters/outbound/persistence}/<context>/` — **no AUTO-MAINTAINED block.** Pure hand-written. Established convention from commit ae70209 (`docs(claude-md): add nested claude.md per discovery layer`).

## What this skill does — mobile mode

1. **Locate the nested CLAUDE.md.** If none, create from `apps/mobile/src/features/_template/CLAUDE.md`.
2. **Read all files in the folder.**
3. **Regenerate the AUTO-MAINTAINED block** between the markers:
   ```
   <!-- AUTO-MAINTAINED:BEGIN -->
   <!-- AUTO-MAINTAINED:END -->
   ```
4. **Preserve everything outside the markers** verbatim.
5. **Commit:** `docs(claude-md): regenerate <feature> auto-maintained block`.

## What this skill does — backend mode

1. **Locate the nested CLAUDE.md.** If none, create directly (no template — the file is fully hand-written; see existing `services/api/src/altune/domain/discovery/CLAUDE.md` for shape).
2. **Read all `*.py` files in the folder** (and `__init__.py` for re-exports).
3. **Derive sections from the code:**
   - **Title + summary** — one-sentence purpose of the bounded context, e.g. "Pure-Python domain types for unified music search." Cross-link sibling contexts (`catalog/`, etc.) when boundaries are meaningful.
   - **Key terms** — every `class Name` (excluding `_private`) becomes a bullet. Description: extract the docstring's first sentence, otherwise summarize from the constructor signature. Cite the file with `[VERIFIED:Read@<path>]`.
   - **Patterns specific here** — derive from repeated structural choices in the code (e.g., "All async via `AsyncSession`", "Repositories take a session in `__init__`"). One bullet per pattern.
   - **Known gotchas** — extract every `# AIDEV-WARNING:` / `# AIDEV-NOTE:` / `# AIDEV-DECISION:` anchor as a bullet. Add observed gotchas (e.g., mypy `comparison-overlap` warnings, library-specific quirks).
4. **Refresh mode (CLAUDE.md exists, code touched more recently):** re-derive sections 3a-d from current code; merge with existing hand-curated content (prefer to keep existing bullets unless code has diverged from them; add new bullets for new classes/anchors).
5. **Commit:** `docs(claude-md): regenerate <context> bounded-context notes` (mobile commit subject pattern unchanged: `docs(claude-md): regenerate <feature> auto-maintained block`).

## Multi-dir invocation

When invoked by the `stop-claude-md-hygiene` Stop hook with multiple dirs flagged, process them sequentially. One commit per dir or one batch commit (`docs(claude-md): regenerate <list>`) — batch is preferred when ≥3 dirs touched.

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
