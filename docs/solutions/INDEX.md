# Solutions index

Compound-engineering learnings: patterns (not bug instances) captured across sessions.

Add new entries via `/compound-learning`. Periodic consolidation via `/audit-docs`.

## Format

```
- YYYY-MM-DD — <title> — <2-sentence summary>
```

## Entries

_(empty — populated as `/compound-learning` records patterns)_
- 2026-06-10 — TYPE_CHECKING-only imports + untested best-effort stages = silent dead code — mbid enrichment NameError'd on every success for weeks because the success branch had no test and failures were swallowed. First test for a pipeline stage must drive the success branch.
