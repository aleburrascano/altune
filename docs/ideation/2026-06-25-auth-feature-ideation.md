---
date: 2026-06-25
topic: auth-feature
focus: improve auth screens + broader auth feature (UX/capability/resilience)
mode: repo-grounded
---

# Ideation: Auth feature & screens

Ran 6-frame divergent ideation (~48 candidates) on improving the mobile auth feature. The session then recalibrated to a smaller, concrete "signup/account production-readiness" scope (see `docs/specs/auth-hardening/spec.md`). This doc preserves the **survivors not pursued in that spec** as a backlog.

## Grounding Context (Codebase)

Auth = `apps/mobile/src/features/auth/`: shared `AuthForm` shell (email + password), `useSession` (loading|signed-in|signed-out union), `useSignIn`/`useSignUp` (typed result unions, failures collapsed to generic error for anti-enumeration), `AuthGate` redirect. Supabase JWT via `expo-secure-store`; api-client injects Bearer. Dark cobalt design system; `Banner` primitive dormant. `altune://` deep-link scheme configured. Email confirmation currently DISABLED in the Supabase project.

## Pursued in `auth-hardening` spec

Confirm-password + validation, email-confirmation UX, password reset, real error surfacing (+ Banner activation), OAuth (Apple+Google), one shared deep-link handler.

## Ranked Ideas (deferred backlog)

### 1. Passwordless-first (OTP / magic-link), single "Continue" screen
**Description:** One email field → OTP code or magic-link deep link; collapses sign-in/sign-up into one verb. Apple/Google as one-tap accelerators.
**Warrant:** `external:` Supabase `signInWithOtp` auto-creates users; Slack/Notion/Substack default to it. `reasoned:` hooks already collapse failures generically — two doors contradict that posture.
**Rationale:** Deletes password-reset, email-verification, and password-strength concerns entirely.
**Downsides:** Email round-trip friction; competes with the email/password model just hardened. **Confidence:** 80% **Complexity:** Med-High **Status:** Unexplored

### 2. Biometric — app-lock + silent session resurrection
**Description:** Opt-in Face ID app-lock (1Password model); on refresh-token failure, spend one biometric prompt to re-mint the session instead of cold logout.
**Warrant:** `external:` expo-local-authentication composes with existing secure-store. `reasoned:` refresh-failure logout is the most jarring frequent event.
**Rationale:** Turns the worst auth event into a one-tap resume. **Downsides:** New native dep; opt-in flow. **Confidence:** 85% **Complexity:** Med **Status:** Unexplored

### 3. Guest / anonymous-first with claim-in-place
**Description:** Anonymous Supabase session browses Discover; wall moves to first write; "sign up" becomes "claim account" via `linkIdentity`, queue snapshot is the migration payload.
**Warrant:** `external:` Supabase `signInAnonymously` + `linkIdentity`. `direct:` "no guest mode" gap; hero built from cached artwork implies browsable pre-auth content.
**Rationale:** Try-before-credentials; lowers first-run abandonment. **Downsides:** Heaviest — touches AuthGate, api-client interceptor, every write path. **Confidence:** 65% **Complexity:** High **Status:** Unexplored

### 4. Self-healing + offline-tolerant session
**Description:** Split `SessionState` to add a "have-identity-but-no-fresh-token" state; explanatory Banner + one-tap repair on refresh failure; keep library usable offline on a known device.
**Warrant:** `direct:` the documented LEARNING ("no silent blank state") + current silent SIGNED_OUT drop.
**Rationale:** Correctness; most grounded. Partially folded into `auth-hardening` error-surfacing. **Downsides:** Union refinement ripples to AuthGate. **Confidence:** 90% **Complexity:** Med **Status:** Partially pursued

### 5. Multi-user cache isolation (query-key namespace + sign-out reset)
**Description:** Namespace every TanStack query key by user; `queryClient.clear()` in `useSignOut`.
**Warrant:** `reasoned:` every request is user-scoped; without it, account-switch on a shared device serves the previous user's cached data.
**Rationale:** Cheap now, structurally prevents a bug class. **Downsides:** Requires a query-key convention. **Confidence:** 88% **Complexity:** Low **Status:** Unexplored — strong standalone follow-up.

### 6. Living hero = onboarding (the locked visual direction A)
**Description:** Cross-fading real-artwork wall + reactive EQ glyph (gated by `useReduceMotion`); the sign-in screen is the pitch, so no onboarding carousel.
**Warrant:** `reasoned:` hero locked + music-forward; separate onboarding duplicates its job.
**Rationale:** Differentiated first impression; bridges mockup → motion. **Downsides:** Visual build (reanimated, artwork sourcing). **Confidence:** 78% **Complexity:** Low-Med **Status:** Deferred visual follow-up to `auth-hardening`.

## Rejection Summary

| # | Idea | Reason Rejected |
|---|------|-----------------|
| 1 | Sheet-over-content AuthGate | Brainstorm variant of gate design; conflicts w/ pre-auth data fetch |
| 2 | Multi-device session list + revoke-all | Premature pre-launch (few users, identity model unsettled) |
| 3 | Abuse/rate-limit + CAPTCHA UX | Premature; Supabase handles server-side; 429 handling folds into error surfacing |
| 4 | UserId VO / session primitive / AuthForm template / anti-enum primitive / boot orchestrator | Engineering hygiene, mostly premature abstraction (YAGNI 2-consumer bar) |
| 5 | Capture name+timezone at signup | Low value; timezone device-derivable |
| 6 | Concierge rich one-time onboarding | Contradicts minimum-friction / hero-is-onboarding direction |
| 7 | Airline adaptive-trust by device history | Hand-rolled risk scoring is risky + premature |
