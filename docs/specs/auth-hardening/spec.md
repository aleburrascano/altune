# Auth hardening (signup / account production-readiness)

> Spec for `auth-hardening` — version 1, drafted 2026-06-25.
> Authors: solo + Claude.
> Status: Ready-for-plan.

## Problem

The v1 auth feature is the bare minimum: one email field, one password field, one button. There is no confirm-password field (a typo silently locks you out of the account you just made), no email confirmation UX (sign-up succeeds into limbo), no password reset (a forgotten password is a permanent dead-end), no client-side validation (malformed input costs a network round-trip to reject), and the typed error reasons the hooks already define are collapsed to `unknown` and never surfaced. User feedback, verbatim: "Password confirm field / Email confirmation sent" — and the obvious neighbors those imply.

## User value

A user signing up or signing in gets the production-grade basics they expect: they can't fat-finger their password into a lockout, they're told to check their email to confirm, they can recover a forgotten password themselves, malformed input is caught instantly, real failures are explained instead of vanishing, and they can sign in with Apple or Google in one tap.

## Scope tier / MVP cut

This is the **minimal tier** for making the existing auth surface production-solid — no passwordless rewrite, no guest mode, no biometric unlock (all explicitly parked; see Out of scope).

- **Minimal (ship this):** confirm-password field + inline validation (email regex, password policy, confirm-match); real error surfacing (no silent blank states), preserving anti-enumeration; email confirmation UX ("Check your email"); password reset (request → recovery deep link → set new password); OAuth sign-in with Apple + Google. All three callback flows (email-confirm, password-reset, OAuth) ride one shared deep-link handler.
- **Deferred to post-launch:** passwordless / magic-link / OTP, anonymous-guest mode, biometric app-lock & session resurrection, multi-device session list, rate-limit/abuse UX, adaptive trust. Captured in `docs/ideation/2026-06-25-auth-feature-ideation.md`.
- **Justified exceptions:** OAuth (Apple + Google) is a larger rock than the rest but is pulled into this tier **needed now because** the user explicitly chose it in-scope and it shares the deep-link spine, so building it alongside is cheaper than a separate effort.

## Acceptance criteria

Each one is testable and becomes at least one automated test.

1. **AC#1 (confirm field)** — Given the sign-up screen, when the password and confirm-password fields differ, then the submit button is disabled and an inline "Passwords don't match" message shows; when they match (and other rules pass) submit is enabled.
2. **AC#2 (email format)** — Given either auth screen, when the email fails a format check, then an inline email error shows and submit is blocked before any network call.
3. **AC#3 (password policy)** — Given the sign-up screen, when the password is shorter than the configured minimum (default 8), then an inline policy hint shows and submit is blocked. The client minimum mirrors the Supabase server-side policy.
4. **AC#4 (email confirmation)** — Given a successful sign-up while Supabase email-confirmation is enabled (signUp returns `session=null`), when sign-up completes, then the app shows a "Check your email to confirm" state rather than routing into the app.
5. **AC#5 (anti-enumeration preserved)** — Given sign-up with an already-registered email, when sign-up completes, then the UI shows the *same* "Check your email" state as for a new email — the response never reveals whether the address already exists. Sign-in continues to show one generic credential error.
6. **AC#6 (forgot password — request)** — Given the sign-in screen, when the user taps "Forgot password?" and submits their email, then the app calls Supabase `resetPasswordForEmail` and shows a "Check your email" state (same regardless of whether the email exists — AC#5 applies).
7. **AC#7 (forgot password — set new)** — Given the user opens the recovery deep link, when they enter a new password (subject to AC#3 + confirm-match), then the password is updated via Supabase and they land signed-in.
8. **AC#8 (deep-link safety)** — Given an incoming `altune://` URL, when the deep-link handler processes it, then only whitelisted callback paths are acted on; unrecognized paths are ignored.
9. **AC#9 (error surfacing)** — Given any auth submission that fails (invalid credentials, weak password, network, unknown), when the failure returns, then a visible, human-readable status message is shown (via the Banner primitive) — never a silent blank state. Credential failures keep generic wording (AC#5).
10. **AC#10 (OAuth)** — Given either auth screen, when the user taps "Continue with Apple" or "Continue with Google" and completes the provider flow, then the returned identity is exchanged via Supabase `signInWithIdToken` and the user lands signed-in. Both providers ship together (Apple is mandatory alongside Google per App Store Guideline 4.8).

## Out of scope

Explicit non-goals — things people might assume but we're not doing here:

- **Music-forward hero visual (direction A)** — the blurred artwork-wall + EQ-glyph hero locked earlier in the session. Deferred to a follow-up visual slice; mockup preserved under `.superpowers/brainstorm/`. This spec rebuilds the screens functionally; the hero layers on after.
- Passwordless / magic-link / OTP sign-in.
- Anonymous "guest" browse-before-auth and account claiming.
- Biometric app-lock and silent session resurrection on refresh failure.
- Multi-device session list / revoke-all.
- Rate-limiting / CAPTCHA / abuse UX.
- Web auth (iOS + Android only per ADR-0006; secure-store falls back to localStorage on web — unguarded, out of scope).
- Decoding/inspecting the JWT on the client.

## Design considerations

Patterns (per `.claude/rules/design-patterns/` and `.claude/rules/vault-consultation.md`):

- **Discriminated unions for every async surface** [vault: behavioral/state] — `SessionState` and the `SignInResult`/`SignUpResult` unions extend rather than gain nullable fields. New states: sign-up gains `awaiting-confirmation`; a new `useResetPassword` result union and `useOAuth` result union follow the same shape.
- **One deep-link handler = Chain/Adapter at the route boundary** [vault: behavioral/chain-of-responsibility, structural/adapter] — a single handler inspects the incoming `altune://` URL and dispatches the recognized callback (email-confirm, recovery, oauth) to Supabase; unknown paths fall through. URL validation/whitelist per `.claude/rules/frontend/rn-security.md`.
- **Banner as the status surface** [structural/decorator-ish status messaging] — the dormant `@shared/ui/primitives/Banner` becomes auth's status surface, replacing the single inline error line for cross-cutting messages ("check your email", "password updated", failures).
- **Validation as feature-local pure functions** — email/password/confirm validators live in `features/auth/lib/` (feature-local; promote to `shared/lib` only on a 2nd consumer, YAGNI). Client validation is UX; server (Supabase policy) is the security backstop — both surface.
- **Anti-enumeration is load-bearing** — preserve the existing generic-error collapse; sign-up and reset show identical "check your email" regardless of account existence.

High-level approach:

- This is a **mobile-only, client-side** feature in the `auth` vertical slice (`apps/mobile/src/features/auth/`). No Go backend changes — the backend already consumes the Supabase JWT via `auth.RequireUserID`.
- It **does** introduce new screens/hooks within the slice; it does **not** introduce a new bounded context.
- It **does** introduce external dependencies: `expo-auth-session` (+ `expo-web-browser`) for OAuth, and possibly `expo-linking` usage for the deep-link handler — **ADR required** (native auth providers + deep-link redirect contract).

## Dependencies

- **Bounded contexts**: none (mobile-only).
- **Other features**: none — extends `features/auth/` in place.
- **External services**: Supabase Auth (existing). **Non-code prerequisites (dashboard config the developer must do):** (a) enable "Confirm email"; (b) set the server-side password policy to match AC#3; (c) register `altune://` redirect URLs for confirm/recovery/oauth callbacks; (d) register Apple + Google OAuth providers (Apple Developer Service ID + key; Google OAuth client).
- **Library/framework additions**: `expo-web-browser` (via `npx expo install`); `expo-linking` already present. OAuth uses Supabase `signInWithOAuth` + the browser flow — `expo-auth-session` is intentionally not taken on (ADR-0018).

## Risks / open questions

- **Risk: enabling "Confirm email" changes `signUp`'s contract project-wide** (returns `session=null`) — mitigation: ship the `awaiting-confirmation` state (AC#4) in the same change that flips the setting; gate so an un-flipped project still behaves (signUp returning a session still routes in).
- **Risk: deep-link handling conflicts with `AuthGate` redirects** — mitigation: handler resolves the Supabase session *before* `AuthGate` evaluates; reuse the existing `useSegments` guard.
- **Risk: OAuth provider setup is environment-specific and untestable in CI** — mitigation: unit-test the hook/buttons with the SDK mocked; gate live verification behind a dev build with real provider credentials.
- **Open question: exact password policy** (length only vs require symbol/number) — to resolve: pick length-first default (min 8), confirm against whatever is set in the Supabase dashboard.

## Telemetry

- **Log events**: sign-up initiated / awaiting-confirmation; password-reset requested; password updated; OAuth started / completed / failed (provider, no PII). Tie to existing structured logging conventions.
- **Metrics**: sign-up→confirmation completion rate; reset-request→reset-complete rate; OAuth success rate per provider; client-validation rejection counts (how often the round-trip was saved).
- **Alerts**: none pre-launch (deferred per architecture observability notes).

## Related

- ADR-0006 (auth ownership / session restoration), ADR-0008 (design system).
- New ADR (this feature): native OAuth providers + deep-link redirect contract — `docs/adr/NNNN-oauth-and-deeplink-auth.md` (to be written in the plan).
- Predecessor spec: `docs/specs/auth-integration/spec.md`.
- Ideation backlog (deferred ideas): `docs/ideation/2026-06-25-auth-feature-ideation.md`.
- Locked visual direction (deferred): mockups in `.superpowers/brainstorm/`.
