---
name: verify-end-to-end
description: |
  ALWAYS fires after writing or modifying production code in services/api/src/ or apps/mobile/src/.
  ALSO fires when the user says "verify", "make sure it works", "did that break anything", "test that",
  "run the tests", or any variant of "is X working". Runs typecheck + lint + unit + integration +
  slice-affecting e2e and reports each phase's actual output. Does NOT claim "works" without showing
  test output.
when_to_use: |
  Use after any non-trivial code change. Use before /code-review-6-aspect. Use before /git-commit.
---

# Verify end-to-end

## What this skill does

Runs a layered verification stack and reports actual output per phase. Never claims a phase passed without showing the output.

## The phases

Run in this order; bail on first failure (but report what you bailed on, not silently).

### 1. Typecheck

| Stack | Command |
|---|---|
| Backend (Python) | `cd services/api && uv run mypy src tests` |
| Mobile (TS) | `cd apps/mobile && pnpm tsc --noEmit` |

### 2. Lint

| Stack | Command |
|---|---|
| Backend | `cd services/api && uv run ruff check src tests` |
| Mobile | `cd apps/mobile && pnpm eslint src` |

### 3. Format check (non-blocking warning if dirty)

| Stack | Command |
|---|---|
| Backend | `cd services/api && uv run ruff format --check src tests` |
| Mobile | `cd apps/mobile && pnpm prettier --check src` |

### 4. Unit tests

| Stack | Command |
|---|---|
| Backend | `cd services/api && uv run pytest tests/unit -q` |
| Mobile | `cd apps/mobile && pnpm test:unit --run` |

### 5. Integration tests (only if touched code in adapters/persistence/external)

| Stack | Command |
|---|---|
| Backend | `cd services/api && uv run pytest tests/integration -q` |

### 6. E2E (only if touched code in feature paths reachable end-to-end)

| Stack | Command |
|---|---|
| Backend | `cd services/api && uv run pytest tests/e2e -q -k <slice-keyword>` |
| Mobile | `cd apps/mobile && pnpm e2e --tag <slice-keyword>` |

E2E is scoped to the affected slice; we don't run the whole suite on every change (too slow).

## Output format

Report per phase:

```
✓ Typecheck (services/api): passed (0 errors)
✓ Lint (apps/mobile): passed (0 issues)
✗ Unit (services/api): 2 failures
  tests/unit/altune/domain/catalog/test_track.py::test_play_count_increments
  tests/unit/altune/domain/catalog/test_track.py::test_play_count_resets_on_release
  (full output below)
…
```

Always include full output for failures. Never paraphrase.

## When all green

State explicitly: **"All verification passed: typecheck · lint · unit · integration · e2e."**

When failing, do NOT continue to the next phase silently. Either:
- Fix and re-run, OR
- Report and ask user how to proceed.

## Anti-patterns

- "Looks right" / "should work" — banned unless backed by actual test output (cited via `[VERIFIED:Bash@<output>]`).
- Skipping integration tests when adapter code changed.
- Running tests but only showing the summary line. Failures need full traceback.
