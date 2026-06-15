---
name: tdd-red-green-refactor
description: |
  ALWAYS fires when the user moves to implementation — "implement", "build the", "let's code", "let's write",
  "make X work" — for a feature that has a plan (docs/specs/<feat>/plan.md). Enforces strict
  RED-GREEN-REFACTOR per slice. Asks for the failing test first, runs it, then writes minimum code,
  then refactors. The pre-tool-tdd-guard hook blocks writes to src/ without a failing test for the
  function being written.
when_to_use: |
  Use during implementation, one slice at a time. Do NOT use without a plan — run /feature-plan first.
  Bug fixes also use this skill (in bug-fix.md mode: failing-test-reproduces-bug → fix → green).
---

# TDD red-green-refactor

## Mandatory first step

Query the software-architecture-design vault MCP:
1. `mcp__software-architecture-design__vk_search "Test-Driven Development"` for the principle reference.
2. `mcp__software-architecture-design__vk_search "Test Double"` if mocking/faking is involved — pick the right double type.
3. Lift any "anti-patterns" from the vault notes into your test design.

## The loop

For each slice in `docs/specs/<feat>/plan.md`:

### RED — write a failing test

1. Open the test file specified in the slice (`tests/unit/...` or `__tests__/...`).
2. Write the **smallest** failing test that captures the slice's acceptance criterion.
3. **Run the test. Confirm it fails.** Capture the output. (Don't just assume — actually run it.)
4. Commit: `test(<scope>): add failing test for <behavior>`.

### GREEN — minimum code to pass

5. Write the smallest implementation that makes the test pass. **Resist** writing for tests not yet written.
6. **Run the test. Confirm it passes.** Capture the output.
7. Commit: `feat(<scope>): <implementation summary>`.

### REFACTOR — improve without breaking

8. Look at the code (yours and adjacent). Is anything clearer, simpler, more cohesive after this change?
9. Make non-behavior-changing improvements. **Re-run the test after each change.**
10. Commit: `refactor(<scope>): <what improved>` (only if meaningful — skip if green code was already clean).

### Repeat for the next slice.

## When the test doesn't fail in RED

That's a red flag. Either:
- The test isn't testing what you think (vacuous assertion, wrong import, wrong path).
- The behavior already exists (the slice is unnecessary — confirm with user, possibly skip).

Stop. Investigate before moving on.

## When you can't get GREEN

Three steps before flailing:
1. **Read the test failure carefully.** Don't just look at the line number — read the message.
2. **Verify your understanding of the test.** Re-read the assertion. Talk back what it's asserting.
3. **If stuck >5 min, invoke `systematic-debugging`** (from your global skill set). Don't grind.

## TDD anti-patterns (the hook will block some of these)

- **Skipping RED.** Writing implementation + test together. Then "running tests" — they pass because written-to-pass.
- **Implement-then-test.** Same shape, looser. Discipline lost.
- **Vacuous tests** that pass even when broken — `assert result is not None` and nothing else.
- **Test for the framework.** Don't write tests that exercise SQLAlchemy/FastAPI behavior.
- **Refactor without tests passing.** Refactor only on green.

## Sacred-tests interaction

This skill never touches existing tests except to **add** new ones. If a test seems wrong, follow `.claude/rules/tests.md` sacred-tests protocol — get user confirmation before modifying.

## Multi-file slices

If a slice touches multiple files:
- Test file goes first (commit 1).
- Each impl file: commit per file, all "green" after the chain. Bisectability stays clean.
- If a slice produces >3 impl commits, the slice was too big — note it in `/compound-learning` for the next planning pass.

## After all slices done

Hand off to `/verify-end-to-end`, then `/code-review-6-aspect`.
