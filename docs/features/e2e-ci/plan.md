# End-to-end suite in CI: spike plan (#2598)

Status: proposed. Output of the spike; the suite itself is the follow-ups below.

## Problem

No workflow runs end-to-end tests. `apps/mobile/e2e/agent-browser/authed-library.sh` is
labelled NON-PRODUCTION and nothing calls it. Every gate is unit or integration level,
so an app-to-API break (auth, save to acquire to play, search) surfaces only in staging
smoke or by hand.

## Decisions

### 1. Target: Expo web driven by agent-browser

- The web bundle and the `TestAuthBridge` path already exist and work headlessly on a
  plain Linux runner. No emulator, no macOS minutes.
- A native runner (Maestro/Detox) covers device-only behaviour (background audio, share
  sheet), which is not where cross-stack regressions live. Deferred; revisit if a
  native-only journey proves worth its cost.
- Web-only gaps (audio under headless Chromium) are asserted at the state level (player
  shows "playing", position advances), not by hearing sound.

### 2. Backend: compose stack in CI, not staging

- Hermetic and per-PR: staging is shared, mutated by other runs, and cannot test the
  PR's own API changes.
- Reuse `services/go-api/deploy/compose.dev.yml` (`npm run dev:up`) plus the postgres
  service pattern from `test-backend.yml`. go-api runs with a non-prod `ENV` so
  `/test/login` is mounted.
- Third-party providers (metadata, acquisition sources) are stubbed at the provider port
  with fixtures in an e2e compose profile; the suite must not call live providers.
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

1. **Auth + library**: test login lands on the library (the existing script, hardened).
2. **Search**: a query returns results; opening one shows the detail screen.
3. **Save**: save a result to the library; it is still there after a reload.
4. **Acquire to play**: a saved track with a stubbed source acquires, reaches ready, and
   starts playing (the core loop; highest value, highest flake risk).

A fifth (sign-out / auth-gate redirect) only if the four are stable.

### 5. CI shape

- New workflow `e2e.yml`, path-filtered to `apps/mobile/**`, `services/go-api/**` and
  `apps/mobile/e2e/**`; runs on PRs and nightly.
- Informational for two weeks, then a required `gate` input once flake rate is under 2%.
- Upload agent-browser screenshots and the go-api log as artifacts on failure.
- Ubuntu runner, Node 22 (the Supabase client needs a global WebSocket).

## Risks

- Bundler cold-start flake: serve a static export (`expo export -p web`) rather than
  `expo start`.
- Provider stub drift from real payloads: reuse the contract fixtures the adapters
  already test against.

## Follow-up tickets (to file)

1. `type:test` / `area:infra`: `e2e.yml` skeleton: compose stack + static web export +
   the existing library journey, informational.
2. `type:feature` / `area:backend`: e2e compose profile with stubbed providers and a
   fresh-database reset.
3. `type:test`: search and save journeys (after 1, 2).
4. `type:test`: acquire-to-play journey (after 2).
5. `type:test`: guard that a prod-config go-api returns 404 on `/test/login` (check
   whether testauth wiring tests already cover it).
6. `type:chore`: promote e2e to a required check after the flake window (after 1-4).
