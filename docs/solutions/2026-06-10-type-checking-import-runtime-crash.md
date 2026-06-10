---
date: 2026-06-10
session-context: discovery rework follow-up (mbid enrichment / artist images)
tags: [python, typing, testing, silent-failure, best-effort-pipelines]
related-vault: ["wiki/concepts/Test-Driven Development.md"]
---

# TYPE_CHECKING-only imports + untested best-effort stages = silent dead code

## The pattern

A pipeline stage constructed a class at runtime (`SearchResult(...)`) whose import
lived only in the `if TYPE_CHECKING:` block. Every successful execution of that
branch raised `NameError` — but the stage was "best-effort" enrichment whose
failures were invisible, the branch only fired on an external-API success
(MusicBrainz URL-lookup hit), and the stage had **zero tests**. The feature
(mbid enrichment, which gates Fanart.tv artist images) had never worked in
production and nothing noticed.

## When it bites

- Adding runtime logic to a module that uses `from __future__ import annotations`
  + a `TYPE_CHECKING` block — annotations resolve lazily, so the missing import
  only explodes when the *constructor/enum/function* is actually called.
- Best-effort / enrichment / fallback paths whose exceptions are swallowed
  upstream or only surface as a degraded result (missing image, missing field).
- Branches gated on an external condition (API hit, cache hit) that test
  environments never trigger.

## What to do

- The first test for any new pipeline stage must drive its **success branch**
  (the one that mutates the result), not just the skip/empty branches.
- When a name from a `TYPE_CHECKING` block is needed at runtime, move it to the
  runtime imports in the same edit — and re-check after the format hook runs
  (this repo's PostToolUse formatter strips TYPE_CHECKING imports it considers
  unused, including ones added moments before their first usage lands).
- Treat "feature shipped but its effect was never observed" as a red flag:
  verify the observable effect (here: `extras["mbid"]` populated) once, live.

## Why this is true

`from __future__ import annotations` makes all *annotations* strings, so mypy is
satisfied by TYPE_CHECKING imports — but constructor calls are runtime name
lookups. mypy won't flag it (the name IS defined for the type checker), and
ruff's F821 won't either. Only executing the branch catches it.

## Anti-pattern to avoid

Writing tests for a best-effort stage that only assert "nothing crashes when the
resolver returns None" — the all-misses path. That suite stays green while the
all-hits path has never executed anywhere.

## See also

- Commit `feat(application): short-circuit mbid enrichment via mb source` (the fix)
- [vault: wiki/concepts/Test-Driven Development.md]
