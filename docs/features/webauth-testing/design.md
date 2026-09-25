# Web/headless test auth — design

Brief: `docs/webauth-testing.md`. The prerequisite that unblocks the Front-end health bucket. Confirmed picks: **non-prod test-login endpoint + agent-browser on Expo web**.

## Anchor & inherited invariants
- **Anchor:** drive the app headlessly to authenticated screens, without the broken web OAuth flow.
- **Inherited:** non-production-only · scoped to a test user.

## Grounded in
- Mobile auth: Supabase (`apps/mobile/src/shared/auth/supabaseClient.ts`, `useSession.ts`).
- go-api verifies **Supabase JWTs via JWKS** (`internal/auth/verifier.go`, `supabase_jwt.go`); the
  auth middleware (`internal/auth/middleware.go`) takes a `TokenVerifier` — swappable.
- UI testing today: manual `agent-browser` on Expo web.

## Significance
**Significant + security-sensitive** — it is an auth bypass. The **non-prod guard is the invariant
that makes it safe.**

## Design decisions
- **The mechanism:** go-api's middleware already takes a pluggable `TokenVerifier`. In **non-prod
  only**, wire an *additional* test verifier that accepts a **test-signed** token for a dedicated
  test user, alongside the real Supabase verifier. Add a **non-prod-only** `POST /test/login` that
  issues that token for the test user.
  - *Over:* mint a Supabase-signed JWT — rejected, go-api can't sign Supabase's JWKS keys.
  - *Over:* client-side token injection only — rejected, still needs a valid token from somewhere;
    the test verifier is the clean source and keeps the app path real.
- **The guard (the crux):** the test verifier and `/test/login` are **compiled/wired only when a
  non-prod flag is set** (e.g. `cfg.Environment != production`), and refuse if the environment looks
  like prod. A prod build/config has **no** test-login route and **no** test verifier. Testable:
  with prod config, `/test/login` 404s and a test token is rejected.
- **The driver:** `agent-browser` on Expo web. The harness calls `/test/login`, gets the token,
  injects it into the Supabase client's session storage so `useSession` sees a live session, and the
  app renders authed screens. Demo: drive one authed screen (e.g. the library) end to end.

## Architectural invariants (spine)
- **Non-prod only:** the test verifier + `/test/login` cannot exist or succeed in production (a prod-config test asserts absence + rejection).
- Scoped to a single dedicated test user; never a real user's session.

## Slice — two leaves
1. **go-api:** the non-prod test verifier + `POST /test/login`, hard-guarded off in prod (+ the
   prod-absence test). This is the security-critical leaf.
2. **mobile/harness:** inject the token into the Supabase client + an `agent-browser` harness that
   logs in via `/test/login` and drives one authed screen headlessly.

## Note
The Front-end health bucket itself is a **separate, later** epic — shaped once this lands and the
authed-screen driving is proven.
