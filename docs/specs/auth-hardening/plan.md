# Plan — auth-hardening

> Implementation plan for `docs/specs/auth-hardening/spec.md`. TDD per `~/.claude/CLAUDE.md` (failing test → implement → pass). Vertical slices within `apps/mobile/src/features/auth/`.

## Slice ordering rationale

Slices 1–2 are **code-only** (no external config) and fully shippable today — they cover the friend's two bullets' nearest, dependency-free value (confirm field, validation, real errors). Slices 3–6 layer on the deep-link spine and external flows; each names its **non-code prerequisite** (dashboard config) that gates live/E2E verification. Unit tests (SDK mocked) pass regardless.

Legend: 🟢 code-only · 🟡 code lands, live verification needs dashboard config.

---

### Slice 1 — Validation + confirm-password field 🟢
**Goal:** AC#1, AC#2, AC#3. Pure validation + the confirm field; no network.
**Files:**
- `features/auth/lib/validation.ts` (new) — `isValidEmail(s)`, `validatePassword(s, {minLength})`, `passwordsMatch(a,b)`; pure functions returning typed results.
- `features/auth/lib/__tests__/validation.test.ts` (new) — table-driven cases.
- `features/auth/ui/AuthForm.tsx` — add optional `showConfirm` + per-field inline error display; disable submit until valid; keep single-error fallback.
- `features/auth/ui/SignUpScreen.tsx` — pass `showConfirm`; wire validators.
- `features/auth/__tests__/SignUpScreen.test.tsx` — confirm-mismatch disables submit + shows message; valid enables.
- `features/auth/__tests__/SignInScreen.test.tsx` — email-format block (no confirm on sign-in).
**Verify:** `npm test -- auth` green; mismatch/format/short-password all block submit before any SDK call.

### Slice 2 — Real error surfacing via Banner 🟢
**Goal:** AC#9 (+ AC#5 preserved). Surface the existing typed reasons; activate `Banner`.
**Files:**
- `features/auth/ui/AuthForm.tsx` — replace inline error line with `@shared/ui/primitives/Banner` for status (keep `testID="auth-error"` on the danger case for existing tests).
- `features/auth/hooks/useSignIn.ts` / `useSignUp.ts` — map reasons to surfaced copy (network vs generic credential vs weak-password); **do not** surface `already_registered` distinctly (AC#5).
- `features/auth/__tests__/*` — each reason renders a visible, non-empty message; credential wording stays generic.
**Verify:** `npm test -- auth` green; no failure path yields a blank state.

### Slice 3 — Deep-link handler spine 🟡 (prereq: Supabase redirect URLs)
**Goal:** AC#8. One handler for `altune://` auth callbacks.
**Files:**
- `features/auth/hooks/useAuthDeepLink.ts` (new) — subscribe to incoming URLs (`expo-linking`); whitelist `auth/confirm`, `auth/recovery`, `auth/callback`; hand token to Supabase; ignore unknown paths.
- `features/auth/lib/parseAuthLink.ts` (new) + test — pure URL→intent parser (whitelist).
- `app/_layout.tsx` — mount the handler above `AuthGate` so the session resolves first.
- `features/auth/__tests__/parseAuthLink.test.ts` — whitelisted vs ignored paths.
**Verify:** unit tests green; manual `npx uri-scheme open "altune://auth/recovery?..." --ios` routes correctly (after dashboard redirect config).
**Prereq:** register `altune://` redirect URLs in Supabase.

### Slice 4 — Password reset 🟡 (prereq: redirect URLs)
**Goal:** AC#6, AC#7.
**Files:**
- `features/auth/hooks/useResetPassword.ts` (new) — `requestReset(email)` → `resetPasswordForEmail`; result union (idle|pending|sent|error). Anti-enumeration: always `sent`.
- `features/auth/hooks/useUpdatePassword.ts` (new) — `updatePassword(new)` → `supabase.auth.updateUser`.
- `features/auth/ui/ForgotPasswordScreen.tsx` (new) + `app/(auth)/forgot-password.tsx` route.
- `features/auth/ui/SetNewPasswordScreen.tsx` (new) + `app/(auth)/reset-password.tsx` route (reached via recovery deep link).
- `features/auth/ui/AuthForm.tsx` (SignIn variant) — "Forgot password?" link.
- Tests for both hooks + screens (SDK mocked).
**Verify:** unit tests green; live reset email round-trip after prereq.
**Prereq:** redirect URLs (Slice 3 prereq).

### Slice 5 — Email confirmation UX 🟡 (prereq: enable "Confirm email")
**Goal:** AC#4, AC#5.
**Files:**
- `features/auth/hooks/useSignUp.ts` — handle `session === null` → `awaiting-confirmation`; update the result union + header comment (the file already anticipates this).
- `features/auth/ui/CheckEmailScreen.tsx` (new) or Banner-driven state — "Check your email to confirm."
- `features/auth/ui/SignUpScreen.tsx` — route to the check-email state on `awaiting-confirmation`.
- `useAuthDeepLink` (Slice 3) — handle `auth/confirm` callback.
- Tests: signUp returning `session=null` → awaiting-confirmation; already-registered shows identical state (AC#5).
**Verify:** unit tests green; live confirm-email flow after enabling the setting.
**Prereq:** enable "Confirm email" in Supabase + redirect URLs.

### Slice 6 — OAuth (Google; Apple deferred) 🟡 (prereq: provider registration + ADR)
**Goal:** AC#10. Google only — Apple needs a paid developer account; see ADR-0018.
**Files:**
- ADR `docs/adr/0018-oauth-providers-and-deeplink-auth.md` (done) — providers + deep-link redirect contract + the new dep.
- `npx expo install expo-web-browser` (`expo-linking` already present; `expo-auth-session` deliberately not used — see ADR-0018).
- `features/auth/hooks/useOAuth.ts` (new) — Apple + Google via `expo-auth-session`; exchange with Supabase `signInWithIdToken`; result union.
- `features/auth/ui/OAuthButtons.tsx` (new) — "Continue with Apple/Google"; rendered in `AuthForm` (both screens).
- `useAuthDeepLink` — handle `auth/callback`.
- Tests: buttons render on both screens; hook wiring with SDK + auth-session mocked.
**Verify:** unit tests green; live one-tap flow on a dev build with real provider credentials.
**Prereq:** Apple Developer Service ID + key; Google OAuth client; both registered in Supabase. ADR merged.

---

### Slice 7 — Music-forward hero across all auth screens 🟢 (done)
**Goal:** the locked direction-A visual. `ui/hero/` gains `AuthHeroLayout` (full-bleed `ArtworkBackground` of gradient tiles + expo-blur + veil; wordmark + `EqGlyph` + tagline; bottom-anchored form w/ safe-area inset), `GoogleLogo` (official mark via react-native-svg). All five screens render through it; OAuth is a compact pill. Added `expo-blur` (ADR ceremony waived by owner). Static only — never animated. 70 auth tests green (testIDs preserved).

## Non-code prerequisites checklist (developer / dashboard)

- [ ] Supabase: enable **Confirm email** (gates Slice 5 live).
- [ ] Supabase: set **password policy** (min length) to match Slice 1 default (8).
- [ ] Supabase: register **`altune://` redirect URLs** for confirm / recovery / oauth (gates Slices 3–6 live).
- [ ] Google Cloud: **OAuth client (Web type)** → redirect URI = Supabase callback.
- [ ] Supabase: enable the **Google** provider (client ID + secret).
- [ ] ~~Apple~~ — deferred (no paid Apple Developer account; see ADR-0018).

## Deferred (not this plan)

Music-forward hero visual (direction A) and the rest of `docs/ideation/2026-06-25-auth-feature-ideation.md`.
