---
name: screen-ui-ux-audit
description: >
  Audit one screen at a time for UI quality, UX clarity, bugs, missing states, and degraded behavior.
  Act as the auditor brain; load browser-agent-screen-testing when live interaction is needed.
  Return all relevant findings and concrete screen-specific improvements.
---

# Screen UI/UX Audit

Audit and improve ONE screen at a time. Stay scoped to the current screen unless nearby flows are required for context, reproduction, or validation.

## Use this skill for

- UI polish
- UX clarity
- missing indicators or feedback
- accessibility issues
- screen-level bugs
- loading/empty/error/success/disabled/retry/offline states
- repeated actions, refresh issues, stale state, flaky network behavior

## If a live screen exists

Load `browser-agent-screen-testing` when:

- physical interaction is needed
- visual verification is needed
- state transitions must be confirmed
- repeated clicks, refreshes, retries, or degraded conditions may expose issues

This skill decides WHAT to inspect.
The browser skill handles HOW to test it.

## Audit checklist

Determine:

- screen goal
- likely user
- primary action
- secondary actions
- key information needed to succeed

Check:

- clarity, discoverability, learnability
- labels, icons, helper text, warnings, confirmations
- spacing, alignment, typography, hierarchy, consistency
- contrast, readability, truncation, overflow, clipping
- indicators, status, progress, selection, feedback
- loading, empty, partial-data, success, error, disabled, retry, offline, stale-data states
- keyboard/focus/tap target/accessibility issues
- duplicate taps, repeated submissions, stale UI, route/state mismatch, modal/drawer issues
- slow requests, failed requests, retries, interruption, awkward re-entry

Use a first-time-user mindset:

- Will the user know what to do?
- Will they notice the right control?
- Will they understand the result?
- Can they recover if something goes wrong?

Flag:

- unclear purpose
- weak hierarchy
- ambiguous actions
- missing states
- weak or missing feedback
- visually inconsistent components
- broken or awkward interactions
- poor degraded-state behavior

## Output

Return ALL relevant findings for the current screen.

For each finding include:

1. Category
2. Element/region/interaction
3. Issue
4. Why it matters
5. Severity
6. Recommended change

Rules:

- Do not artificially limit findings.
- Do not compress into only highlights if more issues exist.
- Treat styling issues as product issues.
- Treat missing states and weak feedback as real UX defects.
- Prefer concrete findings over generic design advice.
