# End-to-end suite in CI: spike plan (#2598)

Status: in progress. Output of the spike; the suite is tracked as E1-E10 on the #2598 plan comment.

## Problem

Before this work no workflow ran end-to-end tests. The agent-browser auth script was
non-production, nothing called it, and the Playwright suite replaced it. Every gate is unit or integration level,
so an app-to-API break (auth, save to acquire to play, search) surfaces only in staging
smoke or by hand.

## Decisions

### 1. Target: Expo web driven by Playwright Test

- Playwright Test drives the static Expo web build (`expo export -p web --dev`). The
  `TestAuthBridge` path (`src/features/auth/testAuth.ts`) works headlessly on a plain
  Linux runner. No emulator, no macOS minutes.
- A native runner (Maestro/Detox) covers device-only behaviour (background audio, share
  sheet), which is not where cross-stack regressions live. Deferred; revisit if a
  native-only journey proves worth its cost.
- The playback limit is react-native-track-player on web (backed by `shaka-player`)
  under headless Chromium. Playback is asserted at the state level (mini player shows,
  `/v1/audio-urls` returns 200, the audio URL returns 200), never by hearing sound.
- Run it with `cd apps/mobile && npx playwright test -c e2e/playwright.config.ts`.

### 2. Backend: compose stack in CI, not staging

- Hermetic and per-PR: staging is shared, mutated by other runs, and cannot test the
  PR's own API changes.
- `scripts/e2e-stack.sh` brings the stack up. go-api runs with a non-prod `ENV` so
  `/test/login` is mounted.
- Third-party providers are replayed from recorded fixtures (`PROVIDER_REPLAY_ENABLED`)
  and acquisition reads a fixture audio source (`ACQUISITION_FIXTURE_ENABLED`); the suite
  must not call live providers.
- Staging keeps its existing smoke role, post-deploy.

### 3. Test auth: existing `/test/login` + `EXPO_PUBLIC_TEST_AUTH=1`

- `testauth` mints a per-process self-signed token for the fixed test user
  (`11111111-...`); the app bootstraps a session via `bootstrapTestAuth()`. No Supabase
  OAuth in CI. Supabase URL and anon key are only needed to construct the client, so CI
  uses dummy values.
- Guard: the flag is inert when `__DEV__` is false, and prod never constructs `testauth`.
  The e2e job must never receive production secrets.
- Each run starts from a fresh database, so the test user starts empty.

### 4. Journeys (start with 4, keep the suite under ~5 minutes)

1. **Auth + library**: test login lands on the library .
2. **Search**: a query returns results; opening one shows the detail screen.
3. **Save**: save a result to the library; it is still there after a reload.
4. **Acquire to play**: a saved track with a fixture source acquires, reaches ready, and
   starts playing (the core loop; highest value, highest flake risk).

A fifth (sign-out / auth-gate redirect) only if the four are stable.

### 5. CI shape

- Workflow `.gitea/workflows/e2e.yml` with `apps/mobile/e2e/playwright.config.ts`,
  path-filtered to `apps/mobile/**`, `services/go-api/**` and
  `apps/mobile/e2e/**`; runs on PRs and nightly.
- Informational for two weeks, then a required `gate` input once flake rate is under 2%.
- Upload Playwright traces and screenshots and the go-api log as artifacts on failure.
- Ubuntu runner, Node 22 (the Supabase client needs a global WebSocket).

## Risks

- Bundler cold-start flake: serve a static export (`expo export -p web`) rather than
  `expo start`.
- Provider replay drift from real payloads: reuse the contract fixtures the adapters
  already test against.
