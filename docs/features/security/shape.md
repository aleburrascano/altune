# Security (Overseer bucket)

Idea brief. Bucket #5. Built on the plugin spine. An **active** bucket (it probes), unlike the passive ones.

## Vision
Continuous, live proof that the app still rejects the attacks we've been hardening against — the security posture, always green or loudly not.

## The idea
A **scheduled self-test prober**: a fixed suite of **safe** checks fired at go-api's **own** endpoints — unauthenticated requests get 401/403, operator-only routes stay operator-only, rate limits shed load, known-bad inputs are rejected — **fenced to an allowlist of the owner's own hosts**. Renders pass/fail + last-run + any regression.

**Rejected:** real exploitation / fuzzing (risk + noise); external scanners (infra); anything destructive.

## Assumptions
- **[load-bearing]** The checks run safely against the live app with no side effects — read probes + auth-rejection assertions, never state-mutating or destructive calls.

## Scope / non-goals
**In:** a bounded suite of safe self-tests, scheduled, fenced to an own-infra allowlist, a pass/fail panel with last-run.
**Out:** destructive / DoS testing; testing anything not on the allowlist; real credential attacks; auto-remediation.

## Priority
**Must:** the self-test suite + the fence + the panel. **Then:** more checks, scheduling controls.

## Invariants
- **Probes-stay-home:** every probe's target host is validated against an own-infra allowlist; a probe can **never** hit a non-allowlisted host. (Testable: point it off-allowlist → refused.)
- **No destructive / state-mutating probe** — the suite only reads and checks rejections.
- Bounded storage · owner-only · observe-only w.r.t. app data (it tests, never changes data).

## Open questions
The exact initial check set and default schedule cadence — confirm in design.
