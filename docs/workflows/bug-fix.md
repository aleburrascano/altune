# Workflow: bug fix

Lighter variant of `new-feature.md`. The discipline shape is the same: **reproduce-with-test, then fix.**

```
1. Reproduce            → write a failing test that captures the bug
2. /verify-end-to-end   → confirm the test fails (and nothing else does)
3. Diagnose             → /systematic-debugging (global skill) if root cause unclear
4. Fix                  → minimum change to make the test pass
5. /verify-end-to-end   → confirm test passes, suite stays green
6. /compound-learning   → if the bug reveals a pattern, capture it
7. /git-commit          → fix(<scope>): <what>, referencing the reproducing test
```

## Reproduce first (always)

A bug without a failing test is anecdotal. Fix without a test is a hope.

The test should:
- Live in the same file as related tests for the affected unit.
- Use the bug's exact triggering input.
- Assert the **correct** behavior (what should happen), not the current broken behavior.

Naming: `test_<unit>_<scenario>_<expected>` — e.g., `test_track_save_with_empty_title_rejects`.

## Sacred-tests note

If a bug fix requires modifying an existing test (the test was testing the wrong behavior), follow the sacred-tests protocol in `.claude/rules/tests.md`:

1. State explicitly why the test is wrong.
2. Get user confirmation.
3. Commit body: `fix(tests): <what> — <why test was wrong>`.

This is rare. Default assumption: the test is right, the code is wrong.

## Root-cause discipline

When the fix isn't obvious:
- Use `systematic-debugging` (global skill).
- Read the failure carefully — don't skim. The error message often contains the answer.
- Read recent commits to the affected file (`git log -p <file>`) to spot when the regression introduced.

Don't flail. If stuck >10 min: `/common-ground` to surface assumptions; ask the user.

## When to ADR

If the fix exposes a deeper architectural issue (the bug is a category, not an instance) → consider `/adr-write` for the pattern correction. Often this also lands in `docs/solutions/` via `/compound-learning`.

## Commit shape

```
fix(<scope>): <short summary>

The bug: <what was happening>
The cause: <why>
The fix: <approach in 1-2 sentences>

Repro test: tests/unit/altune/<path>/test_<thing>.py::test_<scenario>
```

## When to skip the test

Almost never. The exceptions:

- **Pure cosmetic UI fix** with no observable behavior change (e.g., 1px alignment). Manual verification.
- **Build/CI/tooling fix** where the "test" is "CI passes again." Note this in the commit.

Both still get logged in `git log` with context.
