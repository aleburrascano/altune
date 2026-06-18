---
name: browser-agent-screen-testing
version: 1
description: >
  Use agent-browser CLI to physically test one screen at a time through snapshots, refs, screenshots, and state verification.
  Act as the execution layer for screen audits, not the main UX brain.
  Return all relevant observed behaviors and evidence for the current screen.
---

# Browser Agent Screen Testing

Use this skill when a live screen, local app, staging app, or reproducible browser flow is available and physical interaction is needed.

This skill is the execution layer.
It verifies behavior; it does not replace higher-level UX judgment.

## Use this skill for

- opening a target screen
- capturing snapshots
- interacting with refs
- taking screenshots
- verifying state changes
- reproducing screen-level bugs
- checking visual issues
- testing repeated actions, refreshes, retries, and degraded behavior

## Workflow

1. Open the target screen and wait until it is stable enough to inspect.
2. Capture a snapshot and identify the important actionable refs.
3. Interact deliberately: click, fill, toggle, open, close, navigate, retry.
4. Re-snapshot after every meaningful change.
5. Take screenshots when visual verification is useful.
6. Confirm outcomes instead of assuming the action worked.

## Test where relevant

- primary and secondary actions
- forms and validation
- tabs, menus, drawers, modals
- loading, empty, partial-data, success, error, disabled, retry, offline states
- repeated clicks/taps
- repeated submissions
- refresh mid-flow
- back/forward navigation
- stale state after mutation
- awkward re-entry after interruption

## Flag

- click does nothing
- wrong state after action
- duplicate submission possible
- stale UI remains visible
- missing or weak loading/error/success feedback
- screen gets stuck
- back-navigation breaks flow
- modal/drawer behavior is broken
- layout breaks at tested viewport
- truncation, overlap, clipping, or misalignment
- degraded conditions expose broken behavior

## Output

Return ALL relevant observations for the current screen.

For each observation include:

1. Area or interaction
2. Action performed
3. Observed behavior
4. Expected or healthier behavior if clear
5. Severity
6. Evidence type: snapshot, screenshot, diff, or repeated reproduction

Rules:

- Stay scoped to one screen unless nearby navigation is required.
- Prefer snapshot/ref interaction over brittle guessing.
- Re-snapshot after meaningful changes.
- Verify outcomes instead of assuming them.
- Report raw observations faithfully, even if minor.
- Do not compress findings into only a summary.
