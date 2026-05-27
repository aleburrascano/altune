# ADR-0006: Supabase Auth + offline JWKS verification (supersedes ADR-0004)

- **Status:** Accepted
- **Date:** 2026-05-27
- **Deciders:** solo + Claude
- **Context tags:** [security, auth, tech-stack, layer]
- **Supersedes:** [ADR-0004](0004-multi-tenancy-posture.md)

## Context

ADR-0004 locked altune to a hardcoded dev user (`HARDCODED_USER_ID` in env, `current_user_id` FastAPI dependency, prod-startup guard) as the temporary identity source until the first feature truly needs real auth. The `view-library` slice has shipped, and the next user-visible feature (`view-library` against per-user data) plus everything downstream (`import-tracks-v1`, `track-detail`, `track-playback`) all need a real `user_id` — derived from a real, verified identity — to be useful.

The legacy `music-manager` (Flask + Supabase Auth + OCI-compute disk) demonstrated the shape: Supabase JWT in `Authorization: Bearer …`, verified at the backend boundary, `sub` claim → `user_id` flowing into the application layer. ADR-0004 was deliberately designed so that swapping in real auth is a single-file change (`platform/auth.py`'s `current_user_id` body), with every use case signature unchanged and every persisted row's `user_id NOT NULL` already in place.

This ADR locks in the choices the spec at [docs/specs/auth-integration/spec.md](../specs/auth-integration/spec.md) commits to.

## Decision

1. **Identity provider: Supabase Auth.** A new altune-dedicated Supabase project (separate from the legacy `music-manager` project) issues JWTs via email + password sign-up / sign-in. Mobile clients talk to Supabase directly through `@supabase/supabase-js`; altune's backend exposes no `/auth/*` routes in v1.

2. **JWT verification mode: JWKS (asymmetric).** The backend's `SupabaseJwtVerifier` fetches the project's signing keys from `https://<ref>.supabase.co/auth/v1/keys`, caches them with a 1-hour TTL, and refreshes on `kid` cache-miss. Asymmetric (RS/ES) keys are Supabase's recommended direction for new projects and keep key-rotation operationally cheap. HS256 (shared-secret) is explicitly rejected as the default; it remains available as a fallback if the JWKS work proves disproportionate, but no v1 deploy uses it.

3. **JWT library: `pyjwt[crypto]`.** Mature, simple API, official `PyJWKClient` for JWKS handling. The runner-up `python-jose` is more comprehensive (full JOSE / JWS / JWE / JWK) but altune only needs JWT verification — the smaller surface is the better fit.

4. **Verification contract (independent of env):**
   - signature valid against the project's signing key (looked up by `kid`),
   - `exp` not in the past, with **exactly 30 seconds symmetric leeway** (applied to `exp` and `nbf` if present),
   - `iss` matches `https://<ref>.supabase.co/auth/v1`,
   - `aud` matches the configured value (default `"authenticated"`, the Supabase default),
   - `sub` is a non-empty UUID. The UUID becomes `UserId`.

5. **Settings contract.** `Settings` (in `platform/config.py`) holds `supabase_project_url`, `supabase_jwt_aud`, and **exactly one** of `supabase_jwt_secret` (HS256) or `supabase_jwt_jwks_url` (JWKS). A model_validator enforces the XOR at construction time, independent of `env`. The Pydantic raise propagates through `create_app()` so misconfiguration exits the process before the FastAPI app is constructed (AC#13 in the spec).

6. **Email confirmation: disabled in altune v1.** Supabase's default ("require email confirmation") is turned off in the altune project's Authentication settings. v1 users are small + known (you + friends), and `signUp` returning `session=null` would surprise the AC#1 component test. Email-confirmation is re-enabled when password-reset or any public growth surface lands.

7. **No revocation list on altune.** v1 trusts Supabase's `exp` + refresh-token revocation. There is no per-request check against a denylist on altune.

8. **No RLS on altune's Postgres in v1.** Multi-tenancy stays enforced at the application layer (`WHERE user_id = $1` in repositories, mandated since ADR-0004). RLS is defense-in-depth; a separate future ADR + spec adopts it if/when warranted.

9. **Mobile auth client: `@supabase/supabase-js`.** The mobile app uses Supabase's official JS SDK with its session storage backed by `expo-secure-store` (per `apps/mobile/CLAUDE.md`'s sensitive-storage rule). The SDK's auto-refresh handles short-lived access tokens transparently; on refresh failure the SDK fires `SIGNED_OUT` which the auth-aware root layout uses to route to `/sign-in`. This replaces the would-be separate ADR-0007 from the plan's first draft — folded in here because the mobile client choice is a direct implication of choosing Supabase.

10. **`current_user_id` dependency body swap.** The FastAPI dependency at `platform/auth.py` keeps its signature; its body changes from reading `settings.hardcoded_user_id` to extracting the `Authorization` header, delegating to `app.state.token_verifier.verify(...)`, and returning the resolved `UserId`. The verifier is constructed once in the lifespan and stored on `app.state` (same shape as `app.state.settings`). ADR-0004's `HARDCODED_USER_ID` field + prod-startup guard are removed in Slice 8 of the spec — the natural-401 from token verification replaces the guard's protection.

## Alternatives considered

| Alternative | Why not |
|---|---|
| **HS256 with a shared secret** | Simpler (one env var, no network for verification), but rotating the secret requires a coordinated deploy + a brief window where active sessions are rejected. JWKS rotation is operationally cheap and Supabase encourages it for new projects. |
| **`python-jose[cryptography]`** | More comprehensive JOSE library; covers JWS + JWE + JWK fully. altune only needs JWT verification — `pyjwt[crypto]` ships a narrower, cleaner API for that case. |
| **Backend-proxied sign-up / sign-in** (`POST /v1/auth/signup` on altune's FastAPI) | Adds a place to put per-signup hooks later (provisioning per-user resources, audit logs) at the cost of an extra hop on every auth interaction. Not needed in v1; future spec can add the proxy if a real hook materializes. |
| **`auth0`, `clerk`, or rolling our own auth** | Auth0 / Clerk are mature but introduce a vendor distinct from Supabase (which we're already using for legacy data and OCI-adjacent infrastructure). Rolling our own is solo-developer suicide for v1. |
| **OAuth providers (Google / Apple) in v1** | Adds significant scope (provider configuration, redirect handling, per-platform native flows). Email + password is sufficient for the small known v1 user population. OAuth lands in a future `auth-oauth-v1` spec. |
| **Per-request revocation check against Supabase** | Adds a network call to every authenticated request. The `exp` window + refresh-token revocation cover the common case; compromised-account revocation works at next refresh. |
| **RLS policies on altune's Postgres in v1** | Defense-in-depth and worth adopting eventually, but the v1 application-layer multi-tenancy (mandated by ADR-0004) is the load-bearing protection. Adding RLS expands the spec's surface. |

## Consequences

### What becomes easier

- **Per-user features become tractable.** `import-tracks-v1`, `track-detail`, `track-playback`, friends' accounts — all now have a real `user_id` source.
- **ADR-0004's structural risk goes away.** `HARDCODED_USER_ID` is removed; the prod-startup guard becomes redundant (a 401 from an unauthenticated request is the natural protection).
- **Key rotation is cheap.** JWKS cache absorbs new `kid`s automatically on cache-miss; no deploy needed.
- **Mobile auth UX is conventional.** `@supabase/supabase-js` provides idiomatic sign-up / sign-in / session-restore; the team writes auth code, not auth glue.

### What becomes harder

- **The first hard runtime dependency on a third-party identity provider.** Supabase outage → no new sign-ins (existing sessions ride out via cached JWKS until token refresh). Acceptable for v1 user population; circuit-breaker patterns deferred.
- **Two environments to configure.** `services/api/.env` carries the JWT verification config; `apps/mobile/.env` carries `EXPO_PUBLIC_SUPABASE_URL` + `EXPO_PUBLIC_SUPABASE_ANON_KEY`. Documented in `.env.example` files.
- **`pyjwt[crypto]` + `expo-secure-store` are new runtime dependencies.** Standard `uv add` / `expo install` workflows; lockfiles committed.

### What we're committing to (and the cost to reverse)

- **Supabase Auth as the v1 identity provider.** Reversal requires either (a) bridging to a new provider with user-migration tooling, or (b) building our own auth. Moderate-to-high cost. The vendor choice is the most expensive aspect to revisit.
- **JWKS verification mode.** Switching to HS256 is a one-file change (`SupabaseJwtVerifier`'s key-loading path) + a redeploy. Cheap.
- **`pyjwt[crypto]` vs `python-jose`.** Adapter-internal choice; swap is one file. Cheap.
- **No `/auth/*` proxy on altune backend.** Adding one later is straightforward; not having one in v1 leaves the option open without committing to a specific shape.
- **No RLS on altune's Postgres.** Adding RLS later is a migration + careful policy authoring per table. Moderate cost, but the application-layer mandate (ADR-0004 → this ADR) keeps the v1 enforcement intact in the meantime.

## Implementation notes

The auth-integration spec at [docs/specs/auth-integration/spec.md](../specs/auth-integration/spec.md) carries the full implementation contract. The 20-slice TDD plan at [docs/specs/auth-integration/plan.md](../specs/auth-integration/plan.md) decomposes it. This ADR is referenced by Slices 1, 3a-3c, 4, 5, 8 in the plan.

Pre-feature operational checklist (from the plan):
1. Create the altune Supabase project (separate from legacy `music-manager`).
2. Disable email confirmation in Authentication settings.
3. Capture `EXPO_PUBLIC_SUPABASE_URL` + `EXPO_PUBLIC_SUPABASE_ANON_KEY` for mobile env; `SUPABASE_PROJECT_URL` + `SUPABASE_JWT_JWKS_URL` for backend env.

## Vault references

- `[vault: wiki/concepts/API Gateway Pattern.md]` — auth-offload at the boundary; altune's single FastAPI app applies the "Gateway Offloading: authentication" responsibility even though it isn't a microservices gateway.
- `[vault: wiki/concepts/REST.md]` — JWT bearer fits REST's stateless and layered-system constraints.
- `[vault: wiki/concepts/Hexagonal Architecture.md]` — `TokenVerifier` port in `application/auth/`; `SupabaseJwtVerifier` adapter in `adapters/outbound/auth/`.
- `[vault: wiki/concepts/Bounded Context.md]` — auth is cross-cutting; no `domain/auth/` folder.
- `[vault: wiki/concepts/Twelve-Factor App.md]` — Factor III: secrets in env vars, never in repo.
- `[vault: wiki/concepts/Backend for Frontend Pattern.md]` — explicitly rejected for v1 (single mobile client; mobile talks to Supabase directly).
- `[vault: wiki/concepts/Test Double.md]` — `InMemoryTokenVerifier` is a Fowler-style stub (Slice 2's contribution).

## Related

- Predecessor: [ADR-0004](0004-multi-tenancy-posture.md) — **superseded** by this ADR. Slice 8 removes the field + guard.
- Sibling: [ADR-0002](0002-stack-expo-fastapi.md) — listed auth as a deferred decision; this ADR resolves it.
- Spec: [docs/specs/auth-integration/spec.md](../specs/auth-integration/spec.md)
- Plan: [docs/specs/auth-integration/plan.md](../specs/auth-integration/plan.md)
