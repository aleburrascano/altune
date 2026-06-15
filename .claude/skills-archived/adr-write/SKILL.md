---
name: adr-write
description: |
  Fires when the user says "make an ADR", "document this decision", "ADR for X", or after a
  /brainstorm-tech-choice produces a decision. ALSO auto-fires from Stop hook when keywords like
  "decided", "going with", "instead of", "chose to", "adopt" appear in the session and a clearly
  architectural choice was made. Drafts docs/adr/NNNN-<slug>.md with vault cross-references; user
  approves before commit.
when_to_use: |
  Use whenever an architectural decision is made — tech choice, pattern selection, layer boundary
  change, dependency adoption. Skip for non-architectural decisions (which lint rule to use, etc.).
---

# ADR write

## Mandatory first step

Query the software-architecture-design vault MCP for the decision topic. Cross-link in the ADR's `## References` section. This makes the decision traceable to industry-standard patterns rather than ad-hoc.

```
mcp__software-architecture-design__vk_search "<topic>"
mcp__software-architecture-design__vk_get_note "wiki/concepts/<top-hit>.md"
```

## What this skill does

1. **Determine ADR number.** Next sequential after `ls docs/adr/0*.md | sort | tail -1`. Zero-padded to 4 digits.
2. **Decide slug.** Kebab-case, ≤6 words, names the decision (not the option).
   - Good: `0007-auth-with-supabase`, `0012-replace-redis-with-postgres-listen-notify`
   - Bad: `0007-auth-choice`, `0012-database-stuff`
3. **Copy template** `docs/adr/_template/0000-template.md` → `docs/adr/<NNNN-slug>.md`.
4. **Fill in** from session context. Sections (see template):
   - Status (Proposed → Accepted on approval)
   - Date
   - Context — the problem; what forced the decision
   - Decision — what we're doing (one paragraph, plain language)
   - Alternatives considered — the other options with one-line why-not each
   - Consequences — what becomes easier; what becomes harder; what we're committing to
   - References — vault notes, related ADRs, external links
5. **Show the draft** to the user for review. **Do not commit until approved.**
6. **On approval:** mark status Accepted, commit (`docs(adr): NNNN <slug>`).

## When NOT to write an ADR

- Choosing between two equivalent linters.
- File naming conventions.
- Editor preferences.
- Changes the global CLAUDE.md / project CLAUDE.md already mandate.

Rule of thumb: if the decision could be reversed in <30 minutes with no other code changes, it's probably not ADR-worthy. If it would require touching multiple modules to reverse, it is.

## Status transitions

- **Proposed** — drafted, under review
- **Accepted** — adopted; reflected in code/config
- **Deprecated** — no longer the chosen approach; supersedes pointer required
- **Superseded by ADR-NNNN** — replaced by a newer decision; old ADR stays for history

Never delete an ADR. Mark superseded instead.

## Cross-ADR discipline

When a new ADR supersedes an old one, edit the old ADR's status line to point at the new one. The `docs-reviewer` subagent checks this on review.
