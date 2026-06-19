---
name: screen-ui-ux-audit
version: 1
description: >
  Audit one screen at a time for UI quality, UX clarity, bugs, missing states, and degraded behavior.
  Focus on visual polish, interaction quality, indicators, feedback, accessibility, and resilience.
  Report all relevant findings and concrete improvements for the current screen.
---

# Screen UI/UX Audit

Use this skill to audit and improve a single screen at a time.

The goal is to maximize one screen, not to review the whole app at once. Stay tightly scoped to the current screen unless nearby flows or adjacent screens are directly required for context, reproduction, or validation.

## Scope

Analyze the current screen for:

- visual hierarchy
- spacing, alignment, typography, and consistency
- clarity of primary and secondary actions
- missing indicators, helper text, labels, statuses, warnings, confirmations, or affordances
- empty, loading, error, success, disabled, retry, partial-data, stale-data, and skeleton states
- usability friction for first-time users
- confusing flows or interaction dead ends
- screen-level bugs
- accessibility problems
- repeated actions, refresh issues, and degraded behavior under poor network conditions

## Browser automation integration

When a live screen, local app, staging app, or reproducible browser flow is available, load and use the dedicated browser automation skill rather than relying only on static reasoning.

Invoke the browser automation skill when:

- the current screen needs physical interaction to validate behavior
- visual verification is needed
- state transitions must be confirmed
- loading, error, retry, or offline behavior should be tested
- repeated actions, refreshes, or awkward navigation may expose bugs
- screenshots or snapshots would improve the audit

This skill is the auditor brain.
It decides what to inspect, what to test, and what findings matter.
The browser automation skill is the execution layer for opening the screen, capturing snapshots, interacting with elements, taking screenshots, and validating behavior.

## Core questions

Always ask:

- What is this screen trying to help the user do?
- Who is the likely user of this screen?
- What is the main action?
- Are the secondary actions clear and appropriately de-emphasized?
- Would a new user understand this screen quickly?
- Is anything visually noisy, unclear, cramped, weakly emphasized, or misleading?
- Are any indicators, statuses, helper elements, confirmations, warnings, or explanations missing?
- Are all important UI states handled properly?
- What breaks if the user taps fast, refreshes, navigates awkwardly, loses network, or reaches the screen with incomplete or stale data?

## Review lenses

### 1. Screen purpose

Determine:

- the goal of the screen
- the intended primary action
- the main supporting actions
- the information hierarchy
- the minimum information the user needs to succeed

Flag when:

- the screen goal is unclear
- the main action is hidden or visually weak
- the screen is trying to do too many things at once
- important information is buried
- secondary actions compete too much with the primary action

### 2. UX clarity

Check:

- discoverability
- learnability
- clarity of labels and icons
- navigation clarity
- action feedback
- confirmation and recovery patterns
- user control and freedom
- error prevention
- error recovery

Flag when:

- a user would not know what to do next
- important actions are ambiguous
- icon-only actions are unclear
- labels are too vague
- there is no clear path forward
- mistakes are easy to make
- recovery is difficult or missing

Use a cognitive walkthrough mindset:

- will the user know what to do?
- will the user notice the right control?
- will the user understand the result?
- can the user recover if something goes wrong?

### 3. UI quality

Check:

- spacing rhythm
- typography consistency
- alignment
- visual grouping
- hierarchy and emphasis
- contrast and readability
- truncation, wrapping, overflow, and clipping
- component consistency
- use of whitespace
- overall polish

Flag when:

- spacing is inconsistent
- hierarchy is weak or confusing
- text is too dense or too scattered
- components look mismatched
- the design feels visually unbalanced
- important elements do not stand out enough
- the screen looks unfinished or inconsistent

### 4. Indicators and feedback

Check for the presence and clarity of:

- selected states
- active states
- hover, focus, and pressed states where applicable
- progress indicators
- loading indicators
- status chips or badges
- validation hints
- confirmations
- warnings
- success feedback
- error feedback
- disabled explanations where needed

Flag when:

- the user performs an action and receives weak or no feedback
- system status is unclear
- selection state is ambiguous
- progress is invisible
- disabled controls are unexplained
- indicators exist but are too subtle or visually disconnected

### 5. State coverage

Inspect how the screen behaves in:

- default state
- loading state
- skeleton state
- empty state
- partial-data state
- success state
- error state
- disabled state
- retry state
- offline state
- stale-data state

Flag when:

- a state is missing
- a state exists but is visually poor
- a state exists but does not guide the user
- the transition between states is confusing
- errors do not explain what happened or what to do next

### 6. Accessibility

Check:

- readable contrast
- accessible naming of actions
- clarity of labels
- keyboard reachability
- focus visibility
- semantic clarity where inferable
- tap target size
- whether critical meaning depends only on color
- whether status and error messages are noticeable

Flag when:

- interaction would be difficult for keyboard or low-vision users
- focus is hard to track
- controls are too small
- labels are missing or unclear
- the screen relies too heavily on color alone

### 7. Screen-level bugs and interaction issues

Check:

- duplicate taps or clicks
- repeated submissions
- stale UI after actions
- race conditions visible in the UI
- refresh mid-flow
- route/state mismatch
- modal or drawer issues
- broken back-navigation behavior
- inconsistent behavior after retries
- partial update issues

Flag when:

- the UI displays outdated information
- actions can be triggered multiple times incorrectly
- the screen gets stuck
- the user can land in an impossible or broken state
- state updates are delayed, misleading, or inconsistent

### 8. Degraded and resilience behavior

Check:

- slow request behavior
- failed request behavior
- partial load behavior
- offline behavior where applicable
- retry behavior
- interruption during async work
- awkward re-entry into the screen after interruption

Flag when:

- slow operations provide poor feedback
- failed operations strand the user
- retry paths are unclear
- degraded conditions expose broken layout or broken logic
- the screen behaves badly under flaky network conditions

## Output format

Return all relevant findings for the current screen.

For each finding, include:

1. Category
2. Element, region, or interaction involved
3. Issue
4. Why it is a problem
5. Severity
6. Recommended change

Do not artificially limit findings.
Do not compress the review into only a few highlights if more issues are present.
Do not omit lower-severity issues just because higher-severity ones exist.

## Rules

- Stay focused on one screen at a time.
- Prefer concrete findings over generic design commentary.
- Treat styling issues as product issues.
- Treat missing states and weak feedback as real UX defects.
- Report everything relevant you notice on the current screen.
- Prioritize clarity, usability, consistency, and resilience over decoration.
- Expand to neighboring screens only when necessary for context, reproduction, or validation.
