---
name: frontend-ux-resilience
version: 1
description: >
  Review frontend apps for UX friction, UI bugs, styling issues, and failure behavior under real usage.
  Focus on first-time-user learnability, interaction quality, responsiveness, broken states, and degraded network conditions.
  Output reproducible findings, fixes, and validation ideas for both usability and functionality.
---

# Frontend UX Resilience

Use this skill to review frontend apps as both a new user and a hostile tester trying to break flows, expose visual issues, and find weak states.

## Focus

Analyze:

- first-time-user confusion
- navigation friction
- broken flows and dead ends
- inconsistent labels, icons, spacing, and typography
- truncation, overflow, overlap, and layout breakage
- empty, loading, error, and success states
- duplicate clicks/taps
- stale UI state
- offline, slow, or flaky network behavior
- accessibility and recovery issues

Prefer terms like:

- cognitive walkthrough
- heuristic evaluation
- exploratory testing
- resilience testing
- discoverability
- learnability
- error recovery
- UI consistency
- state-transition bug
- degraded-state behavior

## Core assumptions

Assume:

1. A new user does not know the app’s mental model.
2. Passing happy-path flows does not mean the UI is good.
3. Styling bugs are product bugs.
4. Loading, empty, error, and offline states are first-class UI states.

## Review modes

### Cognitive walkthrough

Act like a first-time user.
For each task, ask:

- Would the user know what to do here?
- Would they notice the correct action?
- Would they understand the result?
- If they make a mistake, can they recover? [web:80][web:37]

### Heuristic review

Inspect the interface for:

- visibility of system status
- match to real-world language
- user control and freedom
- consistency and standards
- error prevention
- recognition over recall
- clarity and minimalism
- error recovery quality [web:93]

### Exploratory testing

Freely navigate and try unusual but realistic behavior:

- wrong order actions
- repeated clicks
- fast switching
- refresh mid-flow
- partial form completion
- leaving and returning
- resizing and rotating
- mixing keyboard, mouse, and touch-style interaction [web:91]

### Resilience testing

Check behavior under:

- offline mode
- slow network
- failed API requests
- delayed responses
- missing images/assets
- stale cached state
- interrupted navigation
- partial data loads [web:81][web:87][web:88]

## Review areas

### UX and navigation

Check:

- onboarding clarity
- discoverability of core actions
- confusing labels or icons
- too many steps
- dead ends
- lack of recovery paths

### Functionality

Check:

- buttons, forms, menus, modals, drawers, tabs
- keyboard interaction
- state updates after actions
- duplicate submission prevention
- refresh behavior
- route/state synchronization

### UI and styling

Check:

- spacing consistency
- typography consistency
- wrapping and truncation
- overflow and clipping
- alignment issues
- broken responsive layouts
- dark/light theme mismatches
- inconsistent component states [web:82][web:88][web:85]

### State coverage

Check:

- empty state
- loading state
- skeleton state
- success state
- validation state
- error state
- offline state
- retry state

## If browser automation is available

Use automation to:

- open the app and traverse core journeys
- click primary and secondary paths
- test multiple viewport sizes
- emulate offline mode
- inject delayed or failed requests
- capture screenshots before/after actions
- compare visible UI issues across states [web:81][web:87][web:88]

If automation is not available, perform a structured inspection from code, screenshots, or described flows.

## Method

1. Identify core user journeys.
2. Run cognitive walkthrough on first-time-user tasks.
3. Run heuristic review on main screens.
4. Run exploratory testing on risky flows.
5. Run resilience tests on degraded conditions.
6. Report issues with repro, impact, and suggested fix.

## Output format

For each finding, return:

1. Category
2. Screen or component
3. Reproduction steps
4. Likely current behavior
5. Why it is bad
6. Severity
7. Recommended change
8. Validation method

Severity:

- Critical
- High
- Medium
- Low

## Prompt style

Examples:

- Review this app as a first-time user and find UX friction.
- Explore this frontend for styling inconsistencies and broken flows.
- Test this app under offline and slow-network conditions.
- Audit all loading, empty, error, and retry states.
- Try to break this UI through repeated actions and odd navigation.

## Anti-patterns

Flag aggressively:

- unclear primary action
- hidden navigation
- no feedback after action
- duplicate submissions
- dead-end error states
- broken responsive layout
- text truncation or overlap
- inconsistent spacing/typography
- loading spinners with no timeout or fallback
- offline/failure states that strand the user
