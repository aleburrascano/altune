# `features/auth/` — local context

Per ADR-0006: this feature owns sign-in / sign-up / sign-out + session
restoration on the mobile client. The backend's `current_user_id`
dependency consumes the JWT this feature obtains via Supabase.

## Layout

The singleton SDK client, `useSession` (+ `SessionState`), `useSignOut`, and
the session-expired store live in `shared/auth/` — promoted because 2+
features consume them (see the promotion notes in each file). This feature
owns the auth *flows*:

- `hooks/useSignIn.ts`, `useSignUp.ts`, `useResetPassword.ts`,
  `useUpdatePassword.ts`, `useOAuth.ts`, `useAuthDeepLink.ts` — thin SDK
  wrappers that surface a typed result union.
- `lib/parseAuthLink.ts` + `lib/completeAuthIntent.ts` — the deep-link spine:
  pure classifier + token-exchange/routing.
- `lib/validation.ts`, `lib/errorCopy.ts`, `lib/isNetworkError.ts` — form
  policy, copy, and the shared transport-failure classifier.
- `ui/` — `AuthGate`, `AuthForm`, the screens, and the `hero/` visuals.

## Conventions

- **No direct `@supabase/supabase-js` value import** — the singleton comes
  from `@shared/auth/supabaseClient`; `import type` is fine.
- **No JWT manipulation** in this feature. The mobile app receives an
  opaque session from the SDK; the backend verifies. Don't decode the
  access token here.
- **Error UI** uses `testID="auth-error"` per AC#3. The exact wording is
  intentionally NOT asserted in tests — see the user-enumeration Risk in
  the spec.
- **UI consumes the design system** (`@shared/ui`, ADR-0008): screens are
  dark, built from `Screen` / `Text` / `Button` / `Wordmark`. They import
  primitives **directly** (`@shared/ui/primitives/Button`), not the barrel,
  so the rendered component tests don't transitively load `expo-image`.

## SSR caveat

`expo-secure-store` falls back to `localStorage` on web (per Expo docs).
altune v1 is iOS + Android only. If a web bundle ever ships, the
`supabaseClient` singleton needs a guard — the spec re-opens this risk
in that scenario.

## e2e (Slice 15a/15b)

Maestro is the chosen e2e runner. Local prerequisite: iOS Simulator OR
Android Emulator. CLI install: `brew install maestro` (macOS) or
equivalent. Flows live under `apps/mobile/e2e/`; the auth-session
persistence flow at `e2e/auth-session-persistence.yaml` is the first.

## Knowledge base

`okf/mobile/auth-feature.md` — read before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).
