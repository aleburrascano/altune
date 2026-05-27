# Auth integration

> Spec for `auth-integration` — version 1, drafted 2026-05-27.
> Authors: solo + Claude.
> Status: Ready-for-plan (clarify-gate passed 2026-05-27, reviewer pass 2).

## Problem

altune ships per-user data through `GET /v1/tracks`, but the "current user" is `HARDCODED_USER_ID` from environment (ADR-0004) — a single fake UUID baked into the FastAPI dependency `current_user_id` at `services/api/src/altune/platform/auth.py`. Every visitor sees the same library; there is no real identity. Until that changes, no further per-user feature can exist (importing the user's tracks from the legacy `music-manager` Supabase, a track-detail screen, playback, friends' accounts) because all of them need a real, verified `user_id` to write against and read from.

The legacy `music-manager` (Flask + Supabase Auth + OCI compute disk) demonstrated that this is a tractable shape: Supabase JWT in the `Authorization` header, verified at the FastAPI boundary, `sub` claim → `user_id` flowing into the application layer. The legacy project's mistakes (optional `user_id` repository parameters, app-layer multi-tenancy without RLS, exception-swallowing auth routes) are already structurally avoided in altune by ADR-0004's `user_id UUID NOT NULL` + mandatory `current_user_id` dependency. The seam for swapping in real auth is already there — only the dependency's body has been hardcoded so far.

This is the first feature that consumes user identity from a real source. It also retires ADR-0004's hardcoded posture, replacing it with a new ADR that locks in Supabase as altune's identity provider.

## User value

Open altune, see a sign-in screen. Sign up with email + password (open registration), or sign in with credentials. The library screen now shows **your** tracks — not a fake user's, not a shared user's. Close and reopen the app — you're still signed in. Tap sign-out — back to the sign-in screen. Try to reach `/library` while signed out — bounced back to `/sign-in`.

For altune's owner specifically, this is the gate that turns the rebuild from a demo against seeded data into a real, multi-tenant app that the owner and their friends can use side by side — each with their own library, isolated by JWT-verified `user_id` at the FastAPI boundary.

## Acceptance criteria

Each is testable. Each becomes at least one automated test.

### User-facing ACs

1. **AC#1 (sign-up creates an active session in the app)** — Given an email + password meeting the Supabase project's policy (v1 = Supabase default ≥6 chars, see Design considerations), when the user submits the sign-up form on the mobile app, then the mobile app holds an active session whose `access_token` decodes (in the app, locally) to a JWT whose `sub` claim is a non-empty UUID, and the app navigates to `/library`. Asserted by a component test using a stubbed Supabase client that returns a successful sign-up response with a fixture JWT. A separate manual smoke (NOT an AC) exercises the real public sign-up endpoint once before declaring shipped, to confirm the wiring against the live Supabase project.

2. **AC#2 (sign-in obtains a session for an existing user)** — Given a user previously registered in the altune Supabase project, when the user submits correct credentials on the sign-in screen, then the mobile app stores a session in `expo-secure-store` whose `access_token.sub` equals that user's UUID and navigates to `/library`. Asserted by a component test using a stubbed Supabase client. A real-Supabase manual smoke complements it (same as AC#1).

3. **AC#3 (sign-in failure shows a designed error and creates no session)** — Given any of: wrong password, unknown email, malformed email, or a network failure reaching Supabase, when the user submits sign-in, then no session is created in `expo-secure-store`, the rendered tree contains a node `testID="auth-error"` with non-empty visible text, and the user remains on `/sign-in`. The error message wording is intentionally not asserted — see the user-enumeration Risk for why the spec deliberately does **not** require the message to distinguish "unknown email" from "wrong password". Asserted by a component test using a mocked Supabase client returning each failure mode.

4. **AC#4 (session persists across app restarts)** — Given a signed-in user, when the app process is killed and relaunched, then the previous session is restored from `expo-secure-store` and the first navigation event after launch is to `/library`, not `/sign-in`. Asserted by an e2e test (Maestro `killApp` + `launchApp` or Detox equivalent; the test fixture picks one and uses it consistently). The "first navigation event" observable is tool-neutral on purpose — it does not depend on whether the runner can observe transient renders.

5. **AC#5 (sign-out clears local state and routes to sign-in)** — Given a signed-in user, when they tap the sign-out control, then (a) the session is removed from `expo-secure-store`, (b) the React Query cache is invalidated (verified by an authenticated query firing a fresh network fetch after sign-out, against a representative authenticated hook supplied by the test fixture — today that hook is `useLibrary` because it is the only authenticated query, but the AC binds to "a representative authenticated hook", not to any specific hook name), and (c) the app routes to `/sign-in`. Asserted by a component test and an e2e test.

6. **AC#6 (protected routes redirect to sign-in when unauthenticated)** — Given no session in `expo-secure-store`, when any non-`(auth)` route is requested (including `/`, `/library`, and any deep link to a future screen), then the app routes to `/sign-in` before that screen mounts. Asserted by component tests at the root `_layout.tsx`.

### Backend behavior ACs

7. **AC#7 (backend returns 401 on missing/malformed/invalid-signature/expired tokens)** — Given a request to `GET /v1/tracks` matching any of: (a) no `Authorization` header, (b) malformed `Authorization` value (e.g., not starting with `Bearer `, empty bearer), (c) JWT with invalid signature (signed by a key the verifier does not trust), (d) JWT past `exp` (with the leeway from the verification config applied), then the response is HTTP 401. The response body shape is intentionally not constrained by this AC (FastAPI default or RFC 7807 both acceptable). Asserted by a parameterized e2e test covering all four inputs. Cases for wrong `iss` / wrong `aud` are covered by AC#9 (adapter-level), because exercising them at the e2e layer requires controlling the JWT-signing key the verifier is configured to trust — which only the adapter integration test does.

8. **AC#8 (per-user data isolation via JWT-derived user_id)** — Given two distinct `UserId` values A and B, each seeded with their own `tracks` rows in altune's Postgres under their respective UUIDs, when the in-process FastAPI app under test has `current_user_id` overridden via `app.dependency_overrides` to yield `UserId(A)` for one client and `UserId(B)` for another (no real Supabase round-trip; the dependency is the seam ADR-0004 already created and this spec preserves), then a call to `GET /v1/tracks` from A's client returns a body whose `items[].id` set is exactly equal to A's seeded ids and contains zero of B's. Same shape as `view-library` AC#4, but now exercising the post-auth-swap dependency rather than the hardcoded one. Asserted by an e2e test seeding both users explicitly and overriding the dependency for each client.

9. **AC#9 (adapter verifies tokens correctly across the failure-mode matrix)** — `SupabaseJwtVerifier` has an integration test that constructs JWTs against a fixture-controlled signing key (the same key the verifier-under-test is configured to trust) and asserts:
   - a JWT with correct signature, valid `iss`, valid `aud`, future `exp`, UUID `sub` → returns `UserId(sub)`;
   - signature mismatch → raises `InvalidTokenError`;
   - `exp` in the past (beyond leeway) → raises `InvalidTokenError`;
   - `iss` ≠ expected → raises `InvalidTokenError`;
   - `aud` ≠ expected → raises `InvalidTokenError`;
   - `sub` missing or not a UUID → raises `InvalidTokenError`.
   No real Supabase round-trip. The mapping `InvalidTokenError` → HTTP 401 is covered by AC#7.

### Engineering ACs (test discipline & cleanup)

10. **AC#10 (hexagonal seam discipline at the dependency)** — The `current_user_id` FastAPI dependency has a unit test that consumes an `InMemoryTokenVerifier` stub yielding either `UserId(...)` or raising `InvalidTokenError`. Both branches are covered: success yields the `UserId`; failure surfaces as the same exception that the HTTP error mapper translates to 401. No Supabase, no DB, no network in this layer.

11. **AC#11 (`/health` remains unauthenticated)** — Given a request to `GET /health` with no `Authorization` header (and with the auth changes from this spec applied), then the response is HTTP 200, unchanged from `view-library`. Asserted by an e2e test. This AC exists because the dependency-swap in `platform/auth.py` is a natural place to accidentally tighten the boundary too far.

12. **AC#12 (ADR-0004 retirement)** — `HARDCODED_USER_ID` is removed from `Settings` in `services/api/src/altune/platform/config.py`; the production-startup guard in `services/api/src/altune/platform/app.py` is removed (its replacement is the natural-401 from token verification); `.env.example` no longer documents `HARDCODED_USER_ID` and now documents `SUPABASE_PROJECT_URL` + the JWT verification config (secret or JWKS URL, decided in the plan); the integration test `tests/integration/test_startup_guard.py` is deleted in the same commit. The new ADR (NNNN-supabase-auth) supersedes ADR-0004. Asserted by absence of references to `HARDCODED_USER_ID` in production source — restricted to `services/api/src/altune/**/*.py` and `apps/mobile/src/**/*.{ts,tsx}`, excluding `docs/adr/`, `docs/specs/`, and any AIDEV-NOTE history.

13. **AC#13 (verifier-mode boot-time XOR check)** — Given env config where **both** `SUPABASE_JWT_SECRET` and `SUPABASE_JWT_JWKS_URL` are set, OR **neither** is set, when the app attempts to construct `Settings` at startup, then construction raises a validation error and the process exits non-zero before the FastAPI app object is created. Asserted by a unit test parameterized over both invalid combinations + one positive control (exactly one of the two set → boots successfully). Makes the "exactly one configured at runtime" claim in Design considerations testable rather than implicit.

## Out of scope

Explicit non-goals. Each is a future spec or future ADR; this spec must not bleed into them.

- **OAuth providers** (Google, Apple, GitHub, Spotify). Future spec `auth-oauth-v1`. Email + password is sufficient for v1.
- **Magic link / passwordless** sign-in. Future spec.
- **Password reset / forgot-password** flow. Future spec; needs email-deliverability decisions.
- **Multi-factor authentication / passkeys.** Future spec.
- **Profile / account-management screens** (edit email, change password, delete account, display name, avatar). Future spec `account-management`.
- **RLS policies on altune's Postgres.** Defense-in-depth. v1 enforces multi-tenancy at the application layer via the existing `WHERE user_id = $1` clause — the same layer ADR-0004 already enforces. Adding RLS is a separate ADR + spec.
- **Data import from legacy `music-manager`.** That is `import-tracks-v1`'s spec. v1 of altune asks every user (you + friends) to re-register; their legacy tracks become visible only after the import spec ships.
- **Migration of existing Supabase user identities** from the legacy `music-manager` project to the new altune project. Out by decision (see Locked decisions). Cross-project user migration, if ever wanted, is its own spec.
- **Playback, streaming, audio file storage.** Future spec `track-playback`.
- **Friends / sharing / social features.** Out of overall product scope for the near horizon.
- **Backend-proxied sign-up/sign-in/sign-out endpoints** on altune's FastAPI. Out by design choice for v1: mobile talks to Supabase directly via `@supabase/supabase-js`. A proxied variant can be added later if we need server-side hooks at sign-up time (e.g., to provision per-user resources at registration). The decision is documented here so future contributors don't add altune-side auth endpoints by reflex.
- **Backend-side anti-abuse** (rate limiting, captcha, IP blocking) on the sign-up flow. Open public sign-up has an obvious abuse vector. Mitigation deferred to a future spec; for v1 the registration surface lives at Supabase and accepts Supabase's defaults. If abuse materializes we will add a captcha + email allowlist via a future spec.
- **Hardening of the sign-in error surface to prevent user enumeration.** Spec accepts the current Supabase default behavior (which may differentiate "user not found" from "wrong password" in some response paths) as a v1 trade-off — see the user-enumeration Risk. A future hardening spec (`auth-harden-v1`) closes this if it becomes meaningful.
- **A `users` table in altune's Postgres** + FK from `tracks.user_id`. ADR-0004 mentioned this as a possible follow-on. Not needed for v1 — the only authoritative user data is in Supabase `auth.users`, and altune trusts the `sub` claim. We can add a `users` table in a future spec if we ever need altune-owned per-user state.
- **Token revocation on the altune backend.** v1 trusts Supabase's `exp` and refresh-token revocation. There is no per-request check against a revocation list on altune. If a token needs to be revoked immediately (e.g., compromised account), an admin revokes it in Supabase; existing tokens become invalid at their next refresh.
- **`/v1/auth/me` endpoint or any altune-side profile endpoint.** Future spec when altune owns per-user state.

## Design considerations

### Locked decisions (from the prior clarify gate — do not re-open in the plan)

1. **A new Supabase project dedicated to altune.** The legacy `music-manager` Supabase project is left untouched; everyone (you + friends) re-registers in the altune project. Cross-project user migration is its own future spec if ever wanted.
2. **Email + password only in v1.** No OAuth, magic link, MFA, passkeys.
3. **Open public sign-up.** Anyone can register. Anti-abuse is deferred.
4. **altune's Postgres stays separate** from the Supabase database. Supabase = identity provider only. altune backend verifies the JWT locally and reads the `sub` claim; it never queries Supabase's `auth.users`.
5. **Mobile talks to Supabase directly** for sign-up / sign-in / sign-out. No altune-side `/auth/*` HTTP routes in v1.
6. **Verification uses an offline JWT signature check** — the verifier holds the key (HS256 shared secret) or fetches+caches it (JWKS). No per-request network call to Supabase.

These six are inputs to this spec, not decisions still to be made.

### Vault references

- [vault: wiki/concepts/API Gateway Pattern.md] — altune is not a microservices gateway, but the *"offload auth at the boundary"* responsibility applies identically. JWT verification is centralized in the single FastAPI dependency `current_user_id` at the inbound HTTP adapter; every router and use case downstream consumes the resolved `UserId`. Application and domain layers never touch the token. This matches the gateway's "Gateway Offloading: authentication" responsibility.
- [vault: wiki/concepts/REST.md] — REST's *stateless* constraint says each request carries all information needed to understand it. JWT bearer tokens are exactly that: the server stores no session state; horizontal scaling and statelessness are preserved. The "Layered System" constraint also applies — clients cannot tell whether `current_user_id` is hardcoded (ADR-0004) or JWT-verified (this spec); the seam is invisible by design.
- [vault: wiki/concepts/Hexagonal Architecture.md] — `TokenVerifier` is a port in `application/auth/`; `SupabaseJwtVerifier` is an outbound adapter in `adapters/outbound/auth/`. The domain layer is unchanged: `UserId` already exists at `services/api/src/altune/domain/shared/user_id.py`; no domain types are added.
- [vault: wiki/concepts/Bounded Context.md] — auth is a **cross-cutting concern**, not a new bounded context in the catalog/library/playback sense. Hence: no `domain/auth/` folder. `application/auth/` (port) + `adapters/outbound/auth/` (impl) + `platform/auth.py` (DI seam, body swapped) are the only new locations.
- [vault: wiki/concepts/Twelve-Factor App.md] — Factor III (Config): credentials live in environment variables, not in the repo. `SUPABASE_*` settings sit in `.env` (gitignored) with placeholders in `.env.example`. Direct input into the secret-leak Risk.
- [vault: wiki/concepts/Backend for Frontend Pattern.md] — explicitly rejected for v1 (single mobile client; auth lives directly between mobile and Supabase, with altune's backend only verifying the JWT).

`[vault: wiki/concepts/Circuit Breaker Pattern.md]` is *not* applied in v1 — see the Supabase-reachability Risk below for the v1 trade-off (small user population, JWKS cache absorbs short outages, no protective circuit needed yet).

### High-level approach (not implementation detail — that's the plan)

- This is a **cross-cutting integration** consumed by the catalog bounded context's existing HTTP boundary. It does **not** require a new aggregate or value object. `UserId` is reused.
- It introduces **one new port** (`TokenVerifier`) and **one new outbound adapter** (`SupabaseJwtVerifier`).
- It introduces **two new external dependencies**: one mobile (`@supabase/supabase-js`), one backend (a JWT verification library — `python-jose[cryptography]` or `pyjwt[crypto]`, picked in the plan).
- The body of `current_user_id` in `platform/auth.py` is swapped — same signature, different implementation. ADR-0004 anticipated this and made it a single-file change.
- Sign-up / sign-in / sign-out flows live on mobile and talk to Supabase directly. altune's backend exposes no `/auth/*` routes.

### JWT verification shape

The verifier must assert all of:
- signature valid against the project's signing key,
- `exp` not in the past, **with exactly 30 seconds of leeway** applied symmetrically to `exp` (and `nbf` if present),
- `iss` matches `https://<project-ref>.supabase.co/auth/v1` (Supabase's standard issuer),
- `aud` matches `authenticated` (Supabase's default audience for signed-in users; configurable via `SUPABASE_JWT_AUD` for forward-compatibility),
- `sub` is a non-empty UUID — that is the `user_id`.

On success the dependency yields `UserId(uuid.UUID(payload["sub"]))`. On any failure the dependency raises `InvalidTokenError` (defined in `application/auth/exceptions.py`); the inbound HTTP error mapper translates it to HTTP 401.

**Verification mode** (HS256 with shared secret vs JWKS with ES256/RS256): chosen in the plan. The spec mandates that exactly one is configured at runtime — `Settings` rejects boot if both or neither are set. Lean: JWKS, because Supabase encourages asymmetric keys for new projects and the rotation property has compounding value across future specs. The shared-secret path remains valid if the JWKS work proves disproportionate; the choice is local to the plan.

**Password policy** for sign-up: altune accepts the Supabase project's default policy unchanged (currently ≥6 characters as of 2026-05). altune does not duplicate the policy in its own code. If Supabase tightens the default, altune's e2e test fixture must use a password that satisfies the new bound; the spec is unaffected.

### Mobile shape (high level — concrete file layout lives in the plan)

- A new `auth/` feature slice owns sign-in and sign-up screens, a session hook, and a Supabase client wrapper. First occupant of `apps/mobile/src/features/auth/`.
- The root `_layout.tsx` becomes auth-aware: subscribes to the session, splashes while loading, redirects to `/sign-in` while signed-out, mounts the rest of the app while signed-in.
- The shared HTTP client (`apps/mobile/src/shared/api-client/`) gains an interceptor that injects `Authorization: Bearer <access_token>` from the current session. The header-merge seam already exists for `ngrok-skip-browser-warning` and is the pattern to follow.
- Session persistence uses `expo-secure-store` (per `apps/mobile/CLAUDE.md`'s sensitive-storage rule), via the Supabase SDK's pluggable storage adapter.

The Supabase client singleton starts in `features/auth/api/` per YAGNI; if a second feature ever imports it, the next spec promotes it to `shared/auth/`.

### Backend shape (high level — concrete file layout lives in the plan)

- New `TokenVerifier` Protocol and `InvalidTokenError` live in `application/auth/`.
- New `SupabaseJwtVerifier` adapter lives in `adapters/outbound/auth/`.
- `platform/auth.py`'s `current_user_id` body is swapped from reading `HARDCODED_USER_ID` to delegating to `TokenVerifier.verify`; signature unchanged.
- `platform/config.py` gains the `SUPABASE_*` settings; `HARDCODED_USER_ID` is removed.
- The inbound HTTP layer registers a mapper from `InvalidTokenError` → HTTP 401. If a single `exception_handlers.py` does not yet exist, this spec creates it (registry pattern; future exceptions append).
- `.env.example` is updated: `HARDCODED_USER_ID` out; Supabase config in.

### Response contract

- `GET /v1/tracks` (and every future authenticated endpoint) now requires `Authorization: Bearer <jwt>`. Success body shape is unchanged from `view-library`.
- Missing or invalid token → HTTP 401. Body: FastAPI default for v1.
- `/health` remains unauthenticated (ADR-0004 already excluded it; AC#11 asserts this remains true).
- No altune HTTP endpoints are added for sign-up / sign-in / sign-out; those are mobile-to-Supabase only.

### Email-confirmation policy

Supabase defaults to *email confirmation required*. For altune v1 we **disable email confirmation** in the Supabase project's Authentication settings, accepting the trade-off explicitly:

- Why disable: friction during development and onboarding for you + friends (no email-delivery setup needed at v1).
- Why acceptable: small known user population. Email confirmation in altune v1 is not a security control; it is identity-of-email verification, and altune relies on the email only as a sign-in handle.
- Why we re-enable later: as soon as password reset or any public-facing growth surface lands, email-of-record verification becomes necessary. The ADR captures this as a v1-only relaxation.

## Dependencies

- **Bounded contexts**: catalog (existing — the HTTP boundary that gains real auth).
- **Other features**: `view-library` (existing — first beneficiary; AC#8 verifies its data isolation now flows from a JWT-derived `user_id` via the swapped dependency).
- **External services**:
  - A new Supabase project dedicated to altune (separate from the legacy `music-manager` Supabase project). The project URL and signing config flow into altune backend env; the project URL + anon key flow into mobile env.
  - The legacy Supabase project is **not** touched by this spec.
- **Library/framework additions**:
  - Backend: a JWT verification library — `python-jose[cryptography]` or `pyjwt[crypto]` (picked in the plan). `/brainstorm-tech-choice` runs if the choice is non-trivial; otherwise documented in the plan.
  - Mobile: `@supabase/supabase-js` (latest stable). `expo-secure-store` if not already a transitive dependency.

## Risks / open questions

- **Risk: JWT verification mode is wrong for the long term.** HS256 with a shared secret cannot rotate keys without coordinated deploys; if Supabase rotates the project signing key (manually triggered, but possible), all in-flight sessions break and the backend stops accepting valid tokens until the secret env is updated and the app redeployed. Mitigation: tentatively pick **JWKS** in the plan. Supabase exposes JWKS at `<project>.supabase.co/auth/v1/keys`; the verifier caches keys with a sane TTL (e.g., 1h) and re-fetches on `kid` mismatch. The shared-secret path remains a fallback; the spec mandates one of the two but does not lock the choice here.

- **Risk: clock skew between altune backend and Supabase issuing servers** causes valid tokens to be rejected. Mitigation: 30 seconds of `exp` leeway, applied symmetrically (see Design considerations / JWT verification shape).

- **Risk: secret / signing-material leaked to git.** Mitigation: per [vault: wiki/concepts/Twelve-Factor App.md] Factor III, all sensitive config lives in env vars; `.env` is gitignored; `.env.example` has placeholder strings only. If a leak-scanner is configured in the repo, Supabase secret patterns are added to its rules; if not, the plan considers adding one.

- **Risk: `expo-secure-store` falls back to `localStorage` on web** (unencrypted). Mitigation: altune mobile is iOS + Android only in v1; web is not a target. If web ever becomes a target, this risk must be revisited in a separate spec.

- **Risk: refresh-token failure leaves the user stuck on a stale screen** if `useSession` does not react. Mitigation: subscribe to `supabase.auth.onAuthStateChange`; on `SIGNED_OUT` (which the SDK emits when refresh fails), invalidate React Query cache and let the root `_layout.tsx` redirect to `/sign-in`. Asserted indirectly by AC#5.

- **Risk: Supabase's `auth.users.id` UUIDs do not match the UUID shape altune's `tracks.user_id` column was tested with.** Both are UUIDs; in practice Supabase uses UUID v4. Mitigation: AC#8 seeds rows under explicit UUIDs and exercises the dependency-overridden seam — if the column rejects the value, the test fails loudly. The end-to-end Supabase round-trip is exercised by the AC#1/AC#2 manual smokes.

- **Risk: open public sign-up is an abuse vector** (spam accounts, resource exhaustion on Supabase free tier, eventual OCI cost when those users would stream audio in a future spec). Mitigation: explicitly accepted for v1 (small known user population; no public marketing). Anti-abuse (captcha, email allowlist, invite codes) is deferred. The plan-reviewer subagent is flagged on this risk so it cannot get lost.

- **Risk: removing `HARDCODED_USER_ID` and the startup guard before all dev environments stop using them** breaks local boot. Mitigation: the plan sequences `.env.example` update + dev-env smoke ahead of any commit removing the variable from `Settings`. The verification step (below) boots a clean env (no `HARDCODED_USER_ID` in `.env`) and proves the app starts using Supabase config alone.

- **Risk: user enumeration via the sign-in error surface.** Best practice is for sign-in failures to not differentiate "unknown email" from "wrong password" — otherwise the form is an oracle for whether an email is registered. Supabase's default response paths may surface this differentiation. v1 accepts the trade-off (small known user population, no public marketing) and the spec deliberately does **not** assert message wording in AC#3 — so a future hardening spec can collapse the messages without breaking this AC. A future `auth-harden-v1` closes this if it becomes meaningful.

- **Risk: Supabase free-tier rate-limits sign-ups (~3/h/IP) and would flake an e2e test against the public endpoint.** Mitigation: AC#1 and AC#2 are component-tested against a stubbed Supabase client in CI. The real public sign-up endpoint is exercised once by a manual smoke before declaring shipped — not in CI. AC#8 / AC#9 / AC#10 do not call Supabase at all (dependency override or fixture-controlled key). The result: CI is independent of Supabase availability and rate limits.

- **Risk: Supabase reachability.** During sign-up / sign-in the mobile app must reach Supabase; during JWT verification the backend must have a fresh JWKS cached (or be using HS256 with the secret in env). If Supabase is unreachable for sign-in, AC#3's "network failure" path applies and the user sees the designed error. If Supabase is unreachable for JWKS refresh and a token comes in signed with a `kid` not in the cache, the verifier rejects the token (closed posture). v1 does not add a circuit breaker — the JWKS cache absorbs short Supabase outages for already-known kids; longer outages cause auth failures, which is acceptable for the v1 user population. A circuit-breaker is reconsidered in a future spec if outages materialize.

- **Risk: terminology drift** — "user", "account", "session" are easy to conflate. Mitigation: this spec adds **no new terms** to `docs/ubiquitous-language.md`. `Session` is a mobile-feature-local Supabase SDK type and stays in `apps/mobile/src/features/auth/types.ts`; `User` is not introduced as a domain entity (no `users` table per Out of scope); `UserId` already exists and is unchanged. If `Account` or `Session` later acquire altune-owned semantics, that warrants their own glossary entries at that point — out of scope here.

- **Open question: HS256 with shared secret vs JWKS with ES256/RS256.** Resolution: chosen in the plan, after a brief `/brainstorm-tech-choice` lookup if needed. Lean: JWKS.

- **Open question: `python-jose[cryptography]` vs `pyjwt[crypto]`** for backend JWT verification. Resolution: in the plan; both are mature; minor differences in JWKS-cache ergonomics. `/brainstorm-tech-choice` runs if the choice is non-trivial.

- **Open question: does sign-up include a "display name" field, or just email + password?** Resolution: email + password only in v1. Display name is part of `account-management`'s future spec.

- **Open question: error-mapping body for 401** — FastAPI's default JSON shape vs RFC 7807 problem-details. Resolution: FastAPI default in v1; align with whatever `view-library` already does for 5xx (which deferred RFC 7807). A future cross-cutting spec can normalize.

## Telemetry

What we log / measure to know this works in production. Logs use structlog per `services/api/CLAUDE.md` and the existing pattern in `view-library`.

### Log events (backend)

- `auth.token_verified` — emitted by the verifier on success. Fields: `user_id`, `request_id`. **DEBUG** level. Production log retention must keep DEBUG short (≤7 days) or filter DEBUG out of long-term retention; `user_id` is PII under GDPR even when verified.
- `auth.token_rejected` — emitted on every failure mode. Fields: `reason` (one of: `missing` | `malformed` | `signature_invalid` | `expired` | `claim_invalid_iss` | `claim_invalid_aud` | `claim_invalid_sub`), `request_id`. **NO `user_id`** — we do not trust an unverified token's claims. **INFO** level (interesting events; many failures may indicate a misconfiguration or an attack).
- `auth.startup_config_validated` — emitted once at app startup confirming Supabase config loaded and the verifier initialized. Fields: `verifier_mode` (`hs256` | `jwks`), `iss_expected`, `aud_expected`. **INFO**. Helps debug "why does my JWT reject" without leaking secrets.
- `auth.jwks_refreshed` — emitted only when the JWKS verifier mode is chosen, each time the verifier refreshes its key cache (cache-miss on a new `kid`, or scheduled refresh). Fields: `kids_added`, `kids_removed`, `cache_age_seconds`. **INFO**.
- `request_id` is the only correlation key across `auth.token_verified` and downstream events (e.g., `tracks_listed`). The same `request_id` flows through `view-library`'s existing telemetry.

### What is never logged

- The user's email address (full or otherwise).
- The raw bearer token, any portion of it (no "first 8 chars" debugging shortcut), or the `access_token` / `refresh_token` from the mobile session.
- The signing secret or JWKS private-key material.
- The JWT payload in full — log only the specific claim being asserted (e.g., `reason="claim_invalid_aud"` is allowed; logging `payload={...}` is not).

### Log events (mobile)

Mobile logging discipline is not yet established by ADR; this spec does not introduce a logging library. Errors surface via the designed error states (AC#3) and `console.warn` in development per `apps/mobile/CLAUDE.md`.

### Metrics / Alerts

Metrics: deferred — no metrics ADR yet (same status as `view-library`'s spec). When that ADR ships, the auth-relevant minimum is the rate of `auth.token_rejected` by reason. Alerts: none in v1; no on-call.

## Related

- `[vault: wiki/concepts/API Gateway Pattern.md]` — auth-offload-at-the-boundary applied to a single-service deployment
- `[vault: wiki/concepts/REST.md]` — JWT bearer fits REST's stateless and layered-system constraints
- `[vault: wiki/concepts/Hexagonal Architecture.md]` — `TokenVerifier` port in `application/auth/`, adapter in `adapters/outbound/auth/`
- `[vault: wiki/concepts/Bounded Context.md]` — auth is cross-cutting; no `domain/auth/` folder
- `[vault: wiki/concepts/Twelve-Factor App.md]` — Factor III (Config) for secret handling
- `[vault: wiki/concepts/Backend for Frontend Pattern.md]` — explicitly rejected for v1
- Predecessor ADR: `docs/adr/0004-multi-tenancy-posture.md` — superseded by the new auth ADR drafted as part of this spec's `/adr-write` step
- Sibling ADR: `docs/adr/0002-stack-expo-fastapi.md` — auth was a deferred decision listed there; this spec resolves it
- Predecessor feature spec: `docs/specs/view-library/spec.md` — the first beneficiary of real auth
- Successor specs (planned, not written): `import-tracks-v1`, `track-detail`, `track-playback`, `account-management`, `auth-oauth-v1`, `auth-harden-v1`
