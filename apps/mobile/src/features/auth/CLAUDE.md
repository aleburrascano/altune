# `features/auth/` — local context

Per ADR-0006: this feature owns sign-in / sign-up / sign-out + session
restoration on the mobile client. The backend's `current_user_id`
dependency consumes the JWT this feature obtains via Supabase.

## Layout

- `api/supabaseClient.ts` — the singleton SDK client. Session storage is
  wired to `expo-secure-store` (NOT AsyncStorage; per spec Risk-#3). If a
  second feature ever imports this, promote to `shared/auth/`.
- `types.ts` — trimmed re-exports of Supabase SDK types + `SessionState`
  discriminated union.
- `hooks/useSession.ts` — subscribes to `supabase.auth.onAuthStateChange`,
  exposes `{ session, status }` for protected-route logic.
- `hooks/useSignIn.ts`, `useSignUp.ts`, `useSignOut.ts` — thin wrappers
  around the SDK that surface a typed error result.
- `ui/SignInScreen.tsx`, `SignUpScreen.tsx`, `SignOutButton.tsx`.
- `__tests__/__mocks__/supabaseClient.ts` — the shared Jest mock that
  every test in this folder (and slices 11–14) uses.

## Conventions

- **No direct `@supabase/supabase-js` import** outside `api/` and
  `types.ts`. Everything else imports the singleton via
  `features/auth/api/supabaseClient`.
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
