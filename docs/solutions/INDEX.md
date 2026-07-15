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
- 2026-06-29 — Kind-blind ranking ties are decided by a coverage proxy, not intent — bare-token queries tie on relevance, so multi-source/RRF arbitrarily buries the artist (vax) or the song (firework). Fix with a cross-kind, kind-gated prominence rung; measure with a kind-specific eval at concurrency-1 on recorded fixtures. Low-prominence residue (witchcraft) is personalization, not ranking.
- 2026-07-06 — Synchronous "backfill the whole library" endpoints don't scale — a per-user backfill doing a rate-limited provider call per row (MB ~1/sec) can't finish in one HTTP request as libraries grow (1,639 tracks ≈ 27.5 min; direct calls timed out), and re-scanning from offset 0 wastes every restart. Make it async (202 + worker), incremental (skip already-resolved), and cache the lookups; run detached on the VM as a stop-gap.
