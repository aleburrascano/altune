# Mobile e2e (Maestro)

End-to-end flows for the altune mobile app, executed with [Maestro](https://maestro.dev/).

## Prerequisites

- iOS Simulator (macOS) OR Android Emulator (any platform) running.
- Maestro CLI installed locally. Quick install:
  - macOS: `brew install maestro`
  - Other: `curl -Ls "https://get.maestro.mobile.dev" | bash`
- App built and installed on the simulator / emulator. Easiest path during dev:
  - `npm start` in `apps/mobile/`, then `i` (iOS) or `a` (Android) to launch.

## Fixture user

The `auth-session-persistence` flow signs in with a known fixture user. Provision
the user once in the altune Supabase project (the same project configured via
`EXPO_PUBLIC_SUPABASE_URL`):

1. Supabase dashboard → Authentication → Users → "Add user" → Email + password.
2. Copy the email and password into `apps/mobile/.env.local`:
   ```
   E2E_FIXTURE_EMAIL=you+e2e@example.com
   E2E_FIXTURE_PASSWORD=hunter2hunter2
   ```
3. Maestro reads these via the `${E2E_FIXTURE_EMAIL}` syntax in the YAML flows.

`.env.local` is gitignored. Don't commit credentials.

## Running

```bash
cd apps/mobile
npm run e2e:auth
```

Or directly:

```bash
maestro test e2e/auth-session-persistence.yaml
```

## Flows

- `_hello.yaml` — smoke test; launches the app and asserts ANY element renders.
  Use to verify the harness + simulator wiring before adding behavioral flows.
- `auth-session-persistence.yaml` — AC#4. Signs in, kills the app, relaunches,
  asserts the first navigation event after launch is to `/library`, not
  `/sign-in`.

## Why Maestro

Lightweight setup (no Detox bridging config), YAML-driven flows, runs against
the same simulator/emulator as `npm start`. Chosen in
`apps/mobile/src/features/auth/CLAUDE.md`.
