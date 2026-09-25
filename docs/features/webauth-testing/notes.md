# Web/headless test auth — capability note

Status: **shipped** (epic #1370). This is the *prerequisite* that unblocks the deferred
**Front-end health** bucket; the prober itself is a separate, later epic.

Brief: `docs/webauth-testing.md` · Design: `docs/webauth-testing-design.md`.

## What now works

A non-production-only path to authenticate a single dedicated test user headlessly, so an
automated driver (`agent-browser` on Expo web) can reach authenticated screens without the
Supabase web OAuth flow that is broken on Expo web.

End to end:

1. **go-api mints/verifies a test token.** When enabled, a `testauth.TestAuth`
   (`services/go-api/internal/auth/adapters/testauth/testauth.go`) is built with a per-process
   `crypto/rand` HMAC-SHA256 key and combined with the real Supabase JWKS verifier
   (`Combine`: test first, cheap local MAC; real verifier on fallback so real tokens keep exact
   existing behaviour, including the JWKS-unavailable 503 path). `POST /test/login`
   (`internal/app/testauth_wiring.go`) takes no credentials and returns a fresh token for the
   test user (`11111111-1111-1111-1111-111111111111`, distinct from any real account and from
   `shared.SystemUserId`).
2. **Mobile injects the session.** `apps/mobile/src/shared/auth/testAuth.ts` calls `/test/login`,
   builds a Supabase `Session`, and writes it into the Supabase client's own storage
   (`injectTestSession`), then `_notifyAllSubscribers('SIGNED_IN', …)` so a mounted `useSession`
   flips to signed-in. `TestAuthBridge` (`apps/mobile/src/features/auth/ui/TestAuthBridge.tsx`)
   runs the bootstrap once at startup, mounted *outside* `AuthGate` so it runs while signed-out.
3. **The harness** drives one authed screen (e.g. library) headlessly via `agent-browser` on
   Expo web.

## How to enable it (local / CI only)

- **go-api:** set BOTH `TEST_AUTH_ENABLED=true` AND `ENV` to a non-prod allowlist value
  (`development` or `test`). Either alone → OFF.
- **mobile:** run a **dev** build (`__DEV__ === true`) with `EXPO_PUBLIC_TEST_AUTH=1`. A release
  build has `__DEV__ === false`, so `isTestAuthEnabled()` is false and Metro strips the path as
  dead code.

## Invariants (the spine) — how each is kept

- **Non-prod only, end to end.**
  - go-api: `config.TestAuthEnabled()` (`internal/shared/config/config.go`) fails **closed** —
    requires the explicit `TEST_AUTH_ENABLED=true` opt-in AND an ENV on an allowlist
    (`nonProdTestAuthEnvs`, trimmed + lower-cased). ENV unset (defaults to `development`),
    unknown, misspelled, whitespace-padded, or `production` → OFF. When off, `buildTestAuthVerifier`
    builds no verifier and mounts no route: `/test/login` 404s and a test-signed token is rejected
    by Supabase alone (401). Proven by `internal/app/testauth_wiring_test.go`
    (`TestWiring_ProdHasNoTestLoginAndRejectsTestTokens`,
    `TestWiring_RealConfigFailsClosedWithoutExplicitOptIn`).
  - mobile: `isTestAuthEnabled()` gates on `__DEV__ === true && EXPO_PUBLIC_TEST_AUTH === '1'`, read
    at call time; release builds strip the whole path.
- **Single dedicated test user.** `Verify` always returns the fixed test identity regardless of the
  token's `sub`, and additionally rejects any token whose `sub` is not the test user. A test token
  can never impersonate a real account.
- **Test signing key confined to non-prod.** Generated per process with `crypto/rand`, never read
  from config, never persisted, never leaves the process. Production never calls `testauth.New`, so
  no key exists there; test tokens use HS256 (symmetric), deliberately not one of Supabase's
  asymmetric JWKS algorithms, so the real verifier can never validate one.

## Gates

Both gates pass on the assembled feature: go-api (build, vet, depguard, strict golangci
`--new-from-rev origin/main`, nilaway ceiling, govulncheck, `go test -race`) and mobile
(`tsc`, `eslint`, `jest --ci`).

## Attack pass (epic-close)

A hostile pass over the whole bypass found **no prod-reachable defect**: real RS256 and
`alg=none` tokens cannot impersonate the test user or a real user; `/test/login` cannot be reached
in prod; the signing key is per-process `crypto/rand` only; and the mobile path is stripped from
release builds. Two **dev-only, non-security** harness-robustness notes were deferred to #1395
(stub refresh token drops the session after 1h with `autoRefreshToken`; injection relies on
Supabase SDK internals).
