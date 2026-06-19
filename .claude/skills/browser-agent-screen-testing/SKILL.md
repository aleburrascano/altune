---
name: browser-agent-screen-testing
version: 1
description: >
  Use agent-browser CLI to physically test one screen at a time through snapshots, refs, screenshots, and state verification.
  Focus on deterministic browser interaction, visual confirmation, and screen-level bug discovery.
  Return raw observations and verified behavior for use by higher-level audit skills.
---

# Browser Agent Screen Testing

Use this skill when a live screen, local app, staging app, or reproducible browser flow is available and physical interaction is needed.

This skill is an execution layer.
It does not replace product judgment or UX analysis.
Its role is to open the screen, inspect it, interact with it, verify behavior, and return reliable observations.

## Tooling model

Use agent-browser CLI as the default browser automation tool.

Prefer snapshot-first interaction:

- open the target URL or app route
- capture a snapshot of the page or screen
- interact using stable refs from the snapshot
- re-snapshot after every meaningful change
- take screenshots when visual verification is useful
- use diffs when needed to confirm what changed

Agent-browser is preferred here because it uses compact accessibility snapshots with stable refs such as `@e1`, `@e2`, and supports screenshots and re-snapshot workflows well for deterministic testing.

## Primary use cases

Use this skill for:

- validating a single screen in a running app
- checking whether actions actually work
- confirming state transitions
- testing loading, error, empty, disabled, success, or retry states
- inspecting visual issues with screenshots
- reproducing screen-level bugs
- checking what happens under repeated interaction
- testing refresh mid-flow
- verifying behavior under slow or failed requests where possible

## Core workflow

### 1. Open the screen

- navigate to the target URL, route, or local dev screen
- wait for the screen to stabilize enough for inspection
- identify whether auth, seed data, or setup is required

### 2. Capture the first snapshot

- use interactive or compact snapshots where appropriate
- prefer a snapshot format that keeps context lean and usable
- identify the important actionable refs
- understand the visible structure before acting

### 3. Interact deliberately

Use browser actions to:

- click primary actions
- click secondary actions
- open drawers, modals, popovers, menus, and tabs
- fill inputs
- toggle controls
- navigate back and forward when relevant
- retry failed actions
- repeat interactions when duplicate-action bugs are plausible

After important actions:

- re-snapshot
- verify what changed
- capture screenshots if visual confirmation is needed

### 4. Verify state transitions

Check whether the screen transitions correctly between:

- default
- loading
- empty
- partial-data
- success
- error
- disabled
- retry
- offline or degraded state where applicable

Do not assume the action worked just because a click was issued.
Confirm the outcome from snapshots, visible text, screenshots, or diffs.

### 5. Stress the screen lightly

Where useful, test:

- repeated rapid clicks
- repeated form submission attempts
- refresh during or after async work
- route changes and return navigation
- closing and reopening overlays
- interaction ordering issues
- failure and retry loops

### 6. Return evidence

Return direct observations about:

- what was visible
- what changed
- what failed
- what became stale
- what looked visually wrong
- what required retries
- what was missing

This skill should produce evidence-rich observations, not high-level redesign advice.

## Snapshot rules

- Prefer interactive snapshots for actionability.
- Re-snapshot after any action that may change the DOM or visible state.
- Do not trust refs across meaningful page changes unless a new snapshot confirms them.
- Use compact snapshots when context needs to stay lean.
- Use annotated screenshots when visual mapping between elements and refs would help.
- Use snapshot or screenshot diffs when needed to verify whether an action had an effect.

## Screenshot rules

Take screenshots when:

- layout quality needs verification
- truncation, overflow, clipping, or alignment is suspected
- modal, drawer, or overlay behavior should be inspected
- the screen appears visually inconsistent
- an error or degraded state should be documented
- before/after comparison would help confirm a finding

Prefer screenshots that support a specific finding instead of capturing everything blindly.

## What to test on a screen

Test as many of these as are relevant:

- primary CTA
- secondary CTA
- tabs
- menus
- forms
- validation
- cancel and back behavior
- modal open and close behavior
- success and failure feedback
- disabled controls
- loading indicators
- retry paths
- navigation to and from the screen
- refresh persistence
- stale state after mutation
- duplicate action protection

## What to flag

Flag:

- click does nothing
- wrong state after action
- duplicate submission possible
- repeated clicks cause breakage
- loading state is missing or weak
- error state is missing or unclear
- success feedback is weak or absent
- stale data remains visible
- screen gets stuck
- back-navigation behaves strangely
- modal/drawer traps or breaks flow
- layout breaks at the tested viewport
- text truncates, overlaps, clips, or wraps badly
- elements are present but hard to understand or hard to reach
- screen looks visually inconsistent with its own components

## Output format

Return all relevant observations from the browser session.

For each observation, include:

1. Area or interaction
2. Action performed
3. Observed behavior
4. Expected or healthier behavior if clear
5. Severity
6. Evidence type, such as snapshot, screenshot, diff, or repeated reproduction

If something could not be verified, say so explicitly.

## Rules

- Stay scoped to one screen unless adjacent navigation is required.
- Prefer deterministic snapshot/ref interaction over brittle guesswork.
- Re-snapshot after meaningful changes.
- Verify outcomes instead of assuming them.
- Report raw observations faithfully, even if they seem minor.
- Do not compress findings into only a summary when more detail exists.
- This skill is an execution tool; screen-level judgment belongs to the higher-level audit skill.
