---
paths:
  - "**/tests/**"
  - "**/__tests__/**"
  - "**/*.test.ts"
  - "**/*.test.tsx"
  - "**/*_test.py"
  - "**/test_*.py"
---

# Tests — sacred and load-bearing

## Sacred-tests rule (restated, project-specific consequences)

Tests are **read-only** unless the prompt explicitly says "modify the test." Default behavior on a failing test: **fix the implementation to match the test**, not the other way around.

If the test really is wrong, you must:
1. State explicitly: "the test is wrong because <reason>"
2. Ask the user to confirm before modifying
3. Include the rationale in the commit body (`fix(tests): <what> — <why test was wrong>`)

The `pre-tool-file-guard` hook blocks edits to test files unless the active prompt contains `[ALLOW-TEST-EDIT: <reason>]`.

## Layout

- `services/api/tests/unit/` — domain + application; in-memory adapters; no I/O.
- `services/api/tests/integration/` — adapters against real-ish dependencies (testcontainers).
- `services/api/tests/e2e/` — full stack via httpx against running app.
- `apps/mobile/src/features/<feat>/__tests__/` — feature-local unit/component tests.
- `apps/mobile/e2e/` — Maestro/Detox flows.

Mirror the source structure: `src/altune/domain/catalog/track.py` → `tests/unit/altune/domain/catalog/test_track.py`.

## Structure (AAA)

```python
def test_track_play_count_increments():
    # Arrange
    track = Track.create(title="…", artist="…", duration_ms=180000)

    # Act
    track.register_play()

    # Assert
    assert track.play_count == 1
    events = track.pull_events()
    assert any(isinstance(e, TrackPlayed) for e in events)
```

One assertion concept per test (multiple assert lines OK if they're the same concept).

## Test doubles

Consult `[vault: wiki/concepts/Test Double.md]`. Choose the right kind:

- **Fake** — working implementation simpler than production (`InMemoryTrackRepository`). **Default choice for unit tests.**
- **Stub** — returns canned data for a specific test scenario.
- **Mock** — verifies *interactions* (calls/args). Use sparingly — overuse couples tests to implementation.
- **Spy** — records calls + has real behavior. Niche.

## Coverage targets

- `domain/`, `application/` — **90%+** line + branch. These are the load-bearing layers.
- `adapters/` — 70%+; integration tests cover most paths.
- UI components — meaningful tests on interactive logic; don't chase coverage on pure presentational components.

## Property-based testing

For domain invariants and value-object behavior, use `hypothesis` (Python) — see `[vault: wiki/concepts/Property-Based Testing.md]`. Cheap insurance for invariant-heavy code.

## Naming

- Python: `test_<thing>_<expected_behavior>` — `test_track_play_count_increments`, `test_register_track_rejects_negative_duration`
- TypeScript: `it('increments play count on register_play', …)` or `test('rejects negative duration', …)`

## Anti-patterns

- Testing the framework (don't write tests that just exercise SQLAlchemy's behavior).
- Vacuous assertions (`assert True`, `assert result is not None` and nothing else).
- Tests that mutate shared state without cleanup.
- Tests with `time.sleep(N)` waiting for async behavior — use proper awaits or event-driven sync.
- Snapshot tests for non-deterministic output.
