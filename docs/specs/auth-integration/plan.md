# auth-integration — implementation plan

Spec: [docs/specs/auth-integration/spec.md](spec.md)
Status: Shipped 2026-05-28. All 20 slices landed (Slice 14b folded into 14a's cache-clear assertion). ADR-0006 supersedes ADR-0004. /verify-end-to-end ceremony green. See git log between d23247c and HEAD for the per-slice trail.

## Slices

Twenty vertical slices, ordered for shippability. Each is 2–5 minutes of implementation (RED test + GREEN impl). TDD discipline per slice: failing test → minimum impl → green → optional refactor. Slices 1–8 are backend; 9a–15b are mobile. The backend can ship and pass tests independently of the mobile work, but user-visible behavior (AC#1–AC#6) only lands once mobile catches up.

## Pre-feature operational checklist

These are *not* engineering slices — they are operational tasks to complete in the altune Supabase project's dashboard before Slice 9a's `useSignUp` integration would otherwise return a confusing `session=null`:

1. **Create the altune Supabase project** (separate from the legacy `music-manager` project).
2. **Disable email confirmation** in the project's Authentication settings (per the spec's Email-confirmation policy decision). If left enabled, `signUp` returns `session=null` and Slice 12's AC#1 test (which expects an immediate signed-in session) fails confusingly.
3. **Capture** `EXPO_PUBLIC_SUPABASE_URL` (project URL) and `EXPO_PUBLIC_SUPABASE_ANON_KEY` (anon/publishable key) for `.env`; capture `SUPABASE_PROJECT_URL` + `SUPABASE_JWT_JWKS_URL` for `services/api/.env`.

### Slice 1: Supabase settings + boot-time XOR validator

- **Acceptance criterion**: AC#13
- **Goal**: `Settings` gains `supabase_project_url`, `supabase_jwt_aud` (default `"authenticated"`), and exactly one of `supabase_jwt_secret` (HS256) or `supabase_jwt_jwks_url` (JWKS). A new `model_validator` enforces the XOR: rejects boot if both are set or neither is set, independent of `env`. AC#13's "before the FastAPI app object is created" guarantee is discharged by Pydantic's natural construction-time raise — `Settings()` raising propagates through `create_app()` because the lifespan constructs `Settings()` at startup. The implementer must NOT wrap `Settings()` in `try/except` anywhere.
- **Files**:
  - `services/api/src/altune/platform/config.py` (edit — add four fields + `_validate_supabase_jwt_mode_xor` validator; leave the existing `_refuse_hardcoded_user_in_production` validator alone for now — Slice 8 removes it)
  - `services/api/tests/unit/altune/platform/test_settings_supabase_xor.py` (new — parametrized over four cases)
- **Failing test first** (RED): `test_settings_rejects_when_both_supabase_jwt_secret_and_jwks_url_set`. Same-slice follow-ons: `test_settings_rejects_when_neither_secret_nor_jwks_url_set`, `test_settings_boots_with_secret_only`, `test_settings_boots_with_jwks_url_only`.
- **Verify** (the integration-test run confirms the new XOR validator did not regress the existing prod-startup guard): `cd services/api && uv run pytest tests/unit/altune/platform/test_settings_supabase_xor.py tests/unit/altune/platform/test_startup_guard.py -v`

### Slice 2: `TokenVerifier` port + `InvalidTokenError` + `InMemoryTokenVerifier` test double

- **Acceptance criterion**: AC#10 foundation (port exists for the consumer test); AC#9 foundation (port exists for the adapter test). Test-double type per [vault: wiki/concepts/Test Double.md] is a **stub** — returns canned responses, no interaction verification.
- **Goal**: abstract `TokenVerifier` Protocol in `application/auth/`; exception `InvalidTokenError` carrying a `reason` enum (one of: `missing` | `malformed` | `signature_invalid` | `expired` | `claim_invalid_iss` | `claim_invalid_aud` | `claim_invalid_sub`); an in-memory test double under `tests/_doubles/` configurable with a `{bearer → UserId}` mapping (success) or a `reason` (failure).
- **Files**:
  - `services/api/src/altune/application/auth/__init__.py` (new, empty)
  - `services/api/src/altune/application/auth/ports.py` (new — `class TokenVerifier(Protocol): async def verify(self, raw_bearer: str) -> UserId: ...`)
  - `services/api/src/altune/application/auth/exceptions.py` (new — `InvalidTokenError` + `TokenRejectReason` enum)
  - `services/api/tests/_doubles/in_memory_token_verifier.py` (new)
  - `services/api/tests/unit/altune/application/auth/test_in_memory_token_verifier.py` (new)
- **Failing test first**: `test_in_memory_verifier_returns_configured_user_id_for_known_bearer`. Follow-ons: `test_in_memory_verifier_raises_invalid_token_error_for_unknown_bearer`, `test_in_memory_verifier_raises_with_configured_reason`.
- **Verify**: `cd services/api && uv run pytest tests/unit/altune/application/auth -v`

### Slice 3a: `SupabaseJwtVerifier` happy path (JWKS, no cache yet)

- **Acceptance criterion**: AC#9 (first three cases of the matrix)
- **Goal**: concrete `SupabaseJwtVerifier` implementing the port. Holds: `iss_expected`, `aud_expected`, and a JWKS-loading function that fetches once at construction (no cache/refresh logic yet — that's Slice 3c). Verifies signature against the loaded JWKS, asserts `sub` is a non-empty UUID, returns `UserId(uuid.UUID(sub))`. Emits `auth.token_verified` (DEBUG, success) / `auth.token_rejected` (INFO, with `reason`) per spec Telemetry.
- **Pre-slice tasks**:
  - `/brainstorm-tech-choice` between `python-jose[cryptography]` and `pyjwt[crypto]`. Quick lookup; both are mature; expected outcome: no ADR (note the decision in the brainstorm doc only).
  - **ADR-0006: Supabase Auth + JWKS verification + supersedes ADR-0004.** Locks in: Supabase Auth as identity provider; altune verifies offline via JWKS (not HS256); 30s symmetric leeway; expected `iss`/`aud`; email-confirmation disabled in altune Supabase project for v1; mobile uses `@supabase/supabase-js` (folded in here, no separate ADR-0007); no `/auth/*` HTTP routes on altune's backend in v1. **Write before this slice starts.**
- **Files**:
  - `services/api/src/altune/adapters/outbound/auth/__init__.py` (new)
  - `services/api/src/altune/adapters/outbound/auth/supabase_jwt_verifier.py` (new — happy path only)
  - `services/api/pyproject.toml` (edit — `uv add` the chosen JWT library; lockfile updated in the same slice)
  - `services/api/tests/integration/test_supabase_jwt_verifier_happy_path.py` (new — uses a fixture-controlled RSA key pair the verifier-under-test is configured to trust; mints valid JWTs)
- **Failing test first**: `test_verifier_returns_user_id_for_valid_jwt`. Same-slice follow-ons: `test_verifier_rejects_signature_mismatch`, `test_verifier_rejects_non_uuid_sub`.
- **Verify**: `cd services/api && uv run pytest -m integration tests/integration/test_supabase_jwt_verifier_happy_path.py -v`

### Slice 3b: `SupabaseJwtVerifier` claim validation matrix

- **Acceptance criterion**: AC#9 (remaining four cases)
- **Goal**: extend the verifier to enforce `exp` (with 30s symmetric leeway), `iss`, and `aud` claims. No new files in `src/` — only the verifier's `verify()` body grows. Integration test gets parameterized over the four scenarios.
- **Files**:
  - `services/api/src/altune/adapters/outbound/auth/supabase_jwt_verifier.py` (edit — add `exp`/`iss`/`aud` checks)
  - `services/api/tests/integration/test_supabase_jwt_verifier_claims.py` (new)
- **Failing test first**: `test_verifier_rejects_expired_jwt_beyond_leeway`. Same-slice follow-ons: `test_verifier_accepts_jwt_within_leeway_window`, `test_verifier_rejects_wrong_iss`, `test_verifier_rejects_wrong_aud`.
- **Verify**: `cd services/api && uv run pytest -m integration tests/integration/test_supabase_jwt_verifier_claims.py -v`

### Slice 3c: JWKS cache + refresh on `kid` miss + `auth.jwks_refreshed` telemetry

- **Acceptance criterion**: AC#9 (production-quality JWKS behavior implied by the matrix); spec Telemetry (`auth.jwks_refreshed`)
- **Goal**: the verifier's JWKS-loading function becomes a cache (1h TTL; refresh on cache-miss when a token arrives with an unknown `kid`). Emits `auth.jwks_refreshed` (INFO) on every refresh with `kids_added`, `kids_removed`, `cache_age_seconds`.
- **Files**:
  - `services/api/src/altune/adapters/outbound/auth/supabase_jwt_verifier.py` (edit — replace one-shot JWKS load with the cache)
  - `services/api/tests/integration/test_supabase_jwt_verifier_jwks_cache.py` (new)
- **Failing test first**: `test_verifier_refreshes_jwks_on_unknown_kid`. Same-slice follow-ons: `test_verifier_emits_jwks_refreshed_event_on_refresh`, `test_verifier_does_not_refetch_within_ttl`.
- **Verify**: `cd services/api && uv run pytest -m integration tests/integration/test_supabase_jwt_verifier_jwks_cache.py -v`

### Slice 4: swap `current_user_id` body to delegate to `TokenVerifier` + consumer unit tests

- **Acceptance criterion**: AC#10
- **Goal**: `platform/auth.py`'s `current_user_id` reads the `Authorization` header, strips the `Bearer ` prefix (case-insensitive), delegates to `TokenVerifier.verify`, returns the `UserId`. **The existing `RuntimeError("HARDCODED_USER_ID is unset; …")` branch in `auth.py:L31-L34` is removed and replaced with `raise InvalidTokenError(reason=missing)` when the header is absent**. No defensive check on `app.state.token_verifier` — that's a boot-time concern (Slice 3a's lifespan wiring) handled once at startup. The verifier instance is on `app.state.token_verifier`, matching the existing `app.state.settings` pattern. The lifespan in `platform/app.py` constructs `SupabaseJwtVerifier` from settings and stores it on `app.state`. **`HARDCODED_USER_ID` still exists in `Settings` after this slice** — Slice 8 retires it.
- **Files**:
  - `services/api/src/altune/platform/auth.py` (edit — replace `settings.hardcoded_user_id` body with verifier-delegation body; signature unchanged; remove the `RuntimeError` branch entirely)
  - `services/api/src/altune/platform/app.py` (edit — in the lifespan, instantiate the verifier and assign to `app.state.token_verifier`)
  - `services/api/tests/unit/altune/platform/test_current_user_id.py` (new — uses `InMemoryTokenVerifier` from Slice 2)
- **Failing test first**: `test_current_user_id_returns_user_id_when_verifier_succeeds`. Follow-ons: `test_current_user_id_raises_invalid_token_error_when_authorization_header_missing`, `test_current_user_id_raises_invalid_token_error_when_verifier_rejects`, `test_current_user_id_strips_bearer_prefix_case_insensitive`.
- **Verify**: `cd services/api && uv run pytest tests/unit/altune/platform/test_current_user_id.py -v`

### Slice 5: HTTP error mapper `InvalidTokenError → 401` + e2e for AC#7

- **Acceptance criterion**: AC#7
- **Goal**: a single `exception_handlers.py` registers a mapper from `InvalidTokenError` to a FastAPI 401 response. Parameterized e2e over the four AC#7 cases: missing header, malformed header, invalid signature, expired token. Wrong-`iss` / wrong-`aud` are NOT exercised here (covered by Slice 3b at the adapter layer).
- **Files**:
  - `services/api/src/altune/adapters/inbound/http/exception_handlers.py` (new if absent — registry pattern; one `register_exception_handlers(app)` function called from `platform/app.py`)
  - `services/api/src/altune/platform/app.py` (edit — call `register_exception_handlers(app)` if not already wired)
  - `services/api/tests/e2e/test_auth_rejection_returns_401.py` (new — parameterized; reuses the fixture key pair from Slice 3a/3b so this e2e can mint controlled tokens)
- **Failing test first**: `test_get_tracks_returns_401_without_authorization_header`. Follow-ons: `test_get_tracks_returns_401_with_malformed_bearer`, `test_get_tracks_returns_401_with_bad_signature_jwt`, `test_get_tracks_returns_401_with_expired_jwt`.
- **Verify**: `cd services/api && uv run pytest -m e2e tests/e2e/test_auth_rejection_returns_401.py -v` (Docker required because the app's lifespan constructs the DB engine even when the test never queries the DB; matches the `view-library` e2e convention)

### Slice 6: e2e — `GET /health` remains unauthenticated

- **Acceptance criterion**: AC#11
- **Goal**: explicit regression-guard test. `GET /health` returns 200 without an `Authorization` header even after the auth swap.
- **Files**:
  - `services/api/tests/e2e/test_health_remains_unauthenticated.py` (new — one test)
- **Failing test first**: `test_health_returns_200_without_authorization_header_after_auth_swap`
- **Verify**: `cd services/api && uv run pytest -m e2e tests/e2e/test_health_remains_unauthenticated.py -v` (Docker required, same reason as Slice 5)

### Slice 7: e2e — per-user data isolation via `app.dependency_overrides`

- **Acceptance criterion**: AC#8. **Layered coverage note:** AC#8 has two layers in altune. `view-library`'s Slice 6 already ships the SQL-WHERE-clause isolation test ([test_get_tracks_isolates_users](../view-library/plan.md)). This slice ships the JWT-derived `user_id` flowing into that WHERE clause — together they cover AC#8 end-to-end.
- **Goal**: e2e test seeds tracks for two distinct UUIDs A and B in altune's Postgres, then overrides `current_user_id` via `app.dependency_overrides` to yield `UserId(A)` for one `TestClient` and `UserId(B)` for another. `GET /v1/tracks` from each client returns only that user's rows.
- **Files**:
  - `services/api/tests/e2e/test_tracks_isolation_post_auth.py` (new — uses testcontainers Postgres, seeds two users with explicit literal UUIDs)
- **Failing test first**: `test_user_a_sees_only_a_tracks_after_auth_swap`. Follow-on: `test_user_b_sees_only_b_tracks_after_auth_swap`.
- **Verify**: `cd services/api && uv run pytest -m e2e tests/e2e/test_tracks_isolation_post_auth.py -v` (Docker required)

### Slice 8: Retire `HARDCODED_USER_ID` (AC#12)

- **Acceptance criterion**: AC#12
- **Goal**: remove the `hardcoded_user_id` field + the `_refuse_hardcoded_user_in_production` validator from `Settings`; delete `tests/unit/altune/platform/test_startup_guard.py`; update `.env.example` (remove `HARDCODED_USER_ID`, add the Supabase env vars); confirm via grep that no production source references the removed field — backend AND mobile (per spec AC#12's scope of `services/api/src/altune/**/*.py` and `apps/mobile/src/**/*.{ts,tsx}`).
- **Files**:
  - `services/api/src/altune/platform/config.py` (edit — remove the field + validator)
  - `services/api/.env.example` (edit — symmetric replacement)
  - `services/api/tests/unit/altune/platform/test_startup_guard.py` (delete)
- **Failing test first**: this is a cleanup slice with no new failing test — the verification is structural. Use `[ALLOW-NO-TEST: AC#12 is cleanup; AC#7/AC#8/AC#9/AC#10/AC#11 are the behavioral guards.]` in the commit body for the `pre-tool-tdd-guard` hook.
- **Verify**:
  - `cd services/api && rg --no-heading "HARDCODED_USER_ID|hardcoded_user_id" src/ tests/` → zero matches.
  - `cd c:/Users/Alessandro/Desktop/altune && rg --no-heading "HARDCODED_USER_ID|hardcoded_user_id" apps/mobile/src/` → zero matches (per spec AC#12's scope; expected to already be zero since mobile never used the env, but the spec asserts it).
  - `cd services/api && uv run pytest` → entire suite green (proves the swap is complete).

### Slice 9a: Mobile Supabase client singleton + types (install + ADR-0006 wraps it in)

- **Acceptance criterion**: AC#2 / AC#3 / AC#4 / AC#5 foundation
- **Goal**: install `@supabase/supabase-js` + `expo-secure-store`; write a singleton client wrapper with the Supabase SDK's pluggable storage adapter pointed at `expo-secure-store`. **The storage adapter wiring is the critical bit** (spec Risk: `expo-secure-store` falls back to AsyncStorage if the adapter is omitted) — the failing test verifies the SDK was constructed with the secure-store adapter, not the default. No `useSession` hook yet (Slice 9b).
- **Pre-slice note**: ADR-0006 from Slice 3a's pre-task already documented `@supabase/supabase-js` as the mobile auth client — no separate ADR-0007 is needed; the convention is set by ADR-0006.
- **Files**:
  - `apps/mobile/package.json` + lockfile (edit — `npx expo install @supabase/supabase-js expo-secure-store`)
  - `apps/mobile/src/features/auth/.gitkeep` (new — creates the feature folder; other slice files populate it)
  - `apps/mobile/src/features/auth/api/supabaseClient.ts` (new — `createClient(url, anonKey, { auth: { storage: secureStoreAdapter, persistSession: true, autoRefreshToken: true, detectSessionInUrl: false } })`)
  - `apps/mobile/src/features/auth/types.ts` (new — re-exports trimmed `Session`, `User` from Supabase SDK)
  - `apps/mobile/src/features/auth/CLAUDE.md` (new — feature-local context; documents the SSR/web caveat from spec Risks, and the manual-jest-mock pattern for `supabaseClient`. **Slice 15a appends an e2e section to this same file** — note it here so the implementer doesn't recreate the file later.)
  - `apps/mobile/src/features/auth/__tests__/supabaseClient.test.ts` (new)
- **Failing test first**: `supabaseClient_uses_expo_secure_store_storage_adapter` (mock `expo-secure-store`, assert SDK construction received the adapter — pins Risk #5).
- **Verify**: `cd apps/mobile && npm test -- features/auth/__tests__/supabaseClient`

### Slice 9b: `useSession` hook + tests

- **Acceptance criterion**: AC#2 / AC#3 / AC#4 / AC#5 (hook is the session machinery the rest consume)
- **Goal**: `useSession` hook subscribing to `supabase.auth.onAuthStateChange`. Exposes `{ session, status }` with `status` as a discriminated union (`loading | signed-in | signed-out`) per `.claude/rules/typescript-frontend.md`.
- **Test-double pattern (load-bearing for slices 11/12/14)**: `useSession.test.ts` uses a manual Jest mock of `features/auth/api/supabaseClient.ts`. The same mock module is reused by slices 11, 12, 14a — write the mock in a shared location (`apps/mobile/src/features/auth/__tests__/__mocks__/supabaseClient.ts`) so later slices import-and-extend rather than duplicate.
- **Files**:
  - `apps/mobile/src/features/auth/hooks/useSession.ts` (new)
  - `apps/mobile/src/features/auth/__tests__/__mocks__/supabaseClient.ts` (new — shared mock; configurable to simulate `signed-in` / `signed-out` / event sequences)
  - `apps/mobile/src/features/auth/__tests__/useSession.test.ts` (new)
- **Failing test first**: `useSession_starts_in_loading_status`. Follow-ons: `useSession_transitions_to_signed_in_on_auth_event`, `useSession_transitions_to_signed_out_on_sign_out_event`.
- **Verify**: `cd apps/mobile && npm test -- features/auth/__tests__/useSession`

### Slice 10: Auth-aware root layout + stub `(auth)` routes + sign-out button mount point

- **Acceptance criterion**: AC#6 (also creates the chrome-mount-point Slice 14a will populate)
- **Goal**: root `_layout.tsx` consumes `useSession`; while `loading` renders a minimal splash; while `signed-out` redirects to `/sign-in`; while `signed-in` mounts the rest of the route tree. The signed-in branch's `<Stack>` declares a `headerRight` slot rendered by a small placeholder component — Slice 14a swaps in the real `<SignOutButton />`. Stub `(auth)/sign-in.tsx` and `(auth)/sign-up.tsx` (titles + cross-links; real forms in Slices 11/12). `(auth)/_layout.tsx` defines the route group so the redirect logic does not target itself.
- **Files**:
  - `apps/mobile/src/app/_layout.tsx` (edit — auth-aware gate via `useSession`; `headerRight` placeholder)
  - `apps/mobile/src/app/(auth)/_layout.tsx` (new — minimal Stack)
  - `apps/mobile/src/app/(auth)/sign-in.tsx` (new — stub: title + link to `/sign-up`)
  - `apps/mobile/src/app/(auth)/sign-up.tsx` (new — stub: title + link to `/sign-in`)
  - `apps/mobile/src/app/__tests__/_layout.test.tsx` (new — uses the shared `supabaseClient` mock from Slice 9b)
- **Failing test first**: `root_layout_redirects_to_sign_in_when_session_status_is_signed_out`. Follow-ons: `root_layout_renders_splash_when_session_status_is_loading`, `root_layout_renders_app_tree_when_session_status_is_signed_in`.
- **Verify**: `cd apps/mobile && npm test -- app/__tests__/_layout`

### Slice 11: Sign-in screen + `useSignIn` hook + designed error state (AC#2, AC#3)

- **Acceptance criterion**: AC#2, AC#3
- **Goal**: `SignInScreen` renders email + password inputs and a submit button; on submit it calls `useSignIn` which delegates to `supabase.auth.signInWithPassword`. Success → session lands in `expo-secure-store` (SDK handles it) → `useSession` flips to `signed-in` → root layout routes to `/library`. Failure (any of: wrong credentials, unknown email, malformed email, network error) → renders `testID="auth-error"` with non-empty text; the user remains on `/sign-in`; no session in secure-store. **No test asserts the wording of the error text** (per AC#3 + the user-enumeration Risk).
- **Files**:
  - `apps/mobile/src/features/auth/ui/SignInScreen.tsx` (new)
  - `apps/mobile/src/features/auth/hooks/useSignIn.ts` (new — wraps SDK call; returns `{ kind: 'ok' } | { kind: 'error', reason: string }`)
  - `apps/mobile/src/app/(auth)/sign-in.tsx` (edit — replace stub with `<SignInScreen />`)
  - `apps/mobile/src/features/auth/__tests__/SignInScreen.test.tsx` (new — uses the shared `supabaseClient` mock)
- **Failing test first**: `sign_in_screen_renders_auth_error_when_credentials_wrong`. Follow-ons: `sign_in_screen_renders_auth_error_on_network_failure`, `sign_in_screen_does_not_persist_session_on_failure`, `sign_in_screen_calls_use_sign_in_with_form_inputs`.
- **Verify**: `cd apps/mobile && npm test -- features/auth/__tests__/SignInScreen`

### Slice 12: Sign-up screen + `useSignUp` hook (AC#1)

- **Acceptance criterion**: AC#1
- **Goal**: `SignUpScreen` renders email + password (no confirm field in v1); on submit, `useSignUp` calls `supabase.auth.signUp`. With email confirmation **disabled** in the Supabase project (per pre-feature checklist), the SDK returns a session immediately → secure-store store → `useSession` flips to `signed-in` → root routes to `/library`. Failure surfaces with `testID="auth-error"` + non-empty text.
- **Files**:
  - `apps/mobile/src/features/auth/ui/SignUpScreen.tsx` (new)
  - `apps/mobile/src/features/auth/hooks/useSignUp.ts` (new)
  - `apps/mobile/src/app/(auth)/sign-up.tsx` (edit — replace stub with `<SignUpScreen />`)
  - `apps/mobile/src/features/auth/__tests__/SignUpScreen.test.tsx` (new)
- **Failing test first**: `sign_up_screen_renders_signed_in_session_on_successful_sign_up`. Follow-ons: `sign_up_screen_renders_auth_error_on_failure`, `sign_up_screen_calls_use_sign_up_with_form_inputs`.
- **Verify**: `cd apps/mobile && npm test -- features/auth/__tests__/SignUpScreen`

### Slice 13: `apiFetch` injects `Authorization: Bearer <access_token>` unconditionally when a session exists

- **Acceptance criterion**: enables AC#7/#8 to fire from the live mobile app (their test coverage exists; real HTTP traffic needs the header)
- **Goal**: `apps/mobile/src/shared/api-client/index.ts` reads the current Supabase session synchronously via `supabase.auth.getSession()` and merges `Authorization: Bearer <access_token>` into the headers it already builds. The injection is **unconditional**: any session yields a header. `/health` (server-side unauthenticated) accepts and ignores the header — no opt-out is needed in v1. If a future endpoint specifically requires a no-auth request, a follow-up spec adds a `skipAuth: true` option to `apiFetch`.
- **Files**:
  - `apps/mobile/src/shared/api-client/index.ts` (edit — add Authorization header merge; import the singleton from `features/auth/api/supabaseClient.ts`)
  - `apps/mobile/src/shared/api-client/__tests__/auth-header.test.ts` (new — uses `fetch` mock + the shared `supabaseClient` mock)
- **Failing test first**: `apiFetch_injects_bearer_token_when_session_has_access_token`. Follow-ons: `apiFetch_omits_authorization_header_when_session_is_null`, `apiFetch_preserves_existing_custom_headers_alongside_authorization`.
- **Verify**: `cd apps/mobile && npm test -- shared/api-client/__tests__/auth-header`

### Slice 14a: `useSignOut` hook + `SignOutButton` (button mounts in root `_layout.tsx` header)

- **Acceptance criterion**: AC#5(a) + AC#5(c) — session cleared + routes to `/sign-in`
- **Goal**: `useSignOut` calls `supabase.auth.signOut` (clears SDK session + secure-store) and then clears the React Query cache (`queryClient.clear()`). `SignOutButton` is a tiny pressable that calls the hook on press. The button replaces the placeholder in root `_layout.tsx`'s `headerRight` from Slice 10 — sign-out chrome lives inside the **auth feature**, not the library feature. After the hook completes, `useSession` flips to `signed-out` and the root layout routes to `/sign-in`.
- **Files**:
  - `apps/mobile/src/features/auth/hooks/useSignOut.ts` (new)
  - `apps/mobile/src/features/auth/ui/SignOutButton.tsx` (new)
  - `apps/mobile/src/app/_layout.tsx` (edit — swap `headerRight` placeholder for `<SignOutButton />`)
  - `apps/mobile/src/features/auth/__tests__/useSignOut.test.ts` (new)
- **Failing test first**: `useSignOut_clears_supabase_session_and_react_query_cache`. Follow-on: `sign_out_button_routes_to_sign_in_after_press`.
- **Verify**: `cd apps/mobile && npm test -- features/auth/__tests__/useSignOut`

### Slice 14b: cache-invalidation regression test (AC#5(b))

- **Acceptance criterion**: AC#5(b) — React Query cache invalidated; a representative authenticated query refetches after sign-out + sign-in
- **Goal**: a regression test that mounts a representative authenticated hook (today: `useLibrary` from the library feature; the test imports it but lives under the **auth** feature's tests because it asserts auth behavior), signs the user out, signs back in, and verifies the hook fires a fresh network fetch — proving the cache was cleared. The test binds to "a representative authenticated hook" per spec AC#5's framing, not specifically to `useLibrary`.
- **Files**:
  - `apps/mobile/src/features/auth/__tests__/cache-invalidation-after-signout.test.tsx` (new — lives under `features/auth/` because the assertion is about auth behavior; the import of `useLibrary` is one-way cross-feature for the test fixture only, which is acceptable per `.claude/rules/typescript-frontend.md`'s "tests may import across features to assemble fixtures")
- **Failing test first**: `representative_authenticated_query_refetches_after_sign_out_and_sign_in`
- **Verify**: `cd apps/mobile && npm test -- features/auth/__tests__/cache-invalidation-after-signout`

### Slice 15a: Maestro toolchain bootstrap (first e2e in `apps/mobile/`)

- **Acceptance criterion**: enables AC#4 (Slice 15b carries the actual flow). Slice 15a is the harness-setup slice — necessary because `apps/mobile/e2e/` does not exist today.
- **Goal**: install Maestro CLI as a dev prerequisite (documented in `apps/mobile/src/features/auth/CLAUDE.md` and the feature's `CLAUDE.md`'s e2e section), create `apps/mobile/e2e/`, add an `npm run e2e:auth` script in `package.json` pointing to a still-empty flow, and add a hello-world flow that exercises only app launch to prove the harness works.
- **Files**:
  - `apps/mobile/package.json` (edit — add `"e2e:auth": "maestro test e2e/auth-session-persistence.yaml"` script)
  - `apps/mobile/e2e/.gitkeep` (new — directory marker)
  - `apps/mobile/e2e/_hello.yaml` (new — minimal flow: launch app, assert any element renders; proves CLI + simulator/emulator + bundle are wired)
  - `apps/mobile/src/features/auth/CLAUDE.md` (edit — document Maestro-vs-Detox choice, local prerequisites: iOS simulator OR Android emulator, `brew install maestro` or equivalent)
- **Failing test first**: this is harness bootstrap; no behavioral test. Use `[ALLOW-NO-TEST: bootstrapping e2e harness; first behavioral flow lands in Slice 15b]` in the commit body.
- **Verify**: `cd apps/mobile && npm run e2e:auth -- --help` returns Maestro's help (proves CLI resolvable). With a simulator/emulator running: `cd apps/mobile && maestro test e2e/_hello.yaml` passes.

### Slice 15b: `auth-session-persistence` Maestro flow (AC#4)

- **Acceptance criterion**: AC#4
- **Goal**: the actual flow. Sign in with a known fixture user (provisioned manually in the altune Supabase project as part of the pre-feature checklist; document the credential in `apps/mobile/e2e/README.md` with a placeholder pointing to `.env.local`), force-close the app via `killApp`, relaunch via `launchApp`, assert the **first navigation event after launch is to `/library`, not `/sign-in`** (the tool-neutral observable per spec AC#4).
- **Files**:
  - `apps/mobile/e2e/auth-session-persistence.yaml` (new — Maestro flow)
  - `apps/mobile/e2e/README.md` (new — explains fixture-user provisioning, env-var loading, how to run locally)
- **Failing test first**: `auth_session_persistence_first_nav_after_restart_is_library` (the Maestro flow itself is the test; the flow name is the test name).
- **Verify**: with a simulator/emulator running: `cd apps/mobile && npm run e2e:auth` passes.

## Risks

Engineering risks the plan-reviewer or per-slice TDD discipline should actively guard against. Operational risks (Supabase project setup, etc.) moved to the Pre-feature operational checklist at the top of this plan.

- **JWKS-vs-HS256 silently inverts.** Spec leans JWKS; ADR-0006 locks it in. Slice 3a's pre-task is the explicit gate — if `/brainstorm-tech-choice` surfaces a reason to fall back to HS256, ADR-0006 records it BEFORE the adapter is written. Do not let the choice drift between the spec, the ADR, and the code.
- **Implementer (me) skips Slice 7's two-user e2e** because Slice 5's parameterized 401 test "feels like coverage". They cover orthogonal axes: Slice 5 = bad-token rejection; Slice 7 = good-token isolation. AC#8 cannot be discharged without Slice 7.
- **Slice 8's grep misses a live reference because the pattern is too narrow.** The grep must cover both `HARDCODED_USER_ID` (env name, all caps) and `hardcoded_user_id` (Pydantic field name, lower). The verify command literalizes the alternation; do not simplify it during implementation.
- **Slice 4 forgets to put the verifier on `app.state`** and tries to construct it per-request — would work but reads JWKS over the network on every call. The lifespan-constructed singleton on `app.state.token_verifier` is the right pattern; match the existing `app.state.settings` shape.
- **Slice 9a ships without the `expo-secure-store` storage adapter** — Supabase SDK then falls back to `AsyncStorage`, which spec Risk-#3 explicitly forbids. The Slice 9a failing test is `supabaseClient_uses_expo_secure_store_storage_adapter` exactly to pin this. Do not let the test be weakened or skipped.
- **Slice 11/12 distinguish "unknown email" from "wrong password" in error text** — accidentally. Spec's user-enumeration Risk accepts the current Supabase default behavior, but tests must NOT assert the wording (only `testID="auth-error"` + non-empty text). Otherwise a future hardening spec that collapses the messages breaks AC#3.
- **`@supabase/supabase-js` SDK crashes on web** when the storage adapter accesses `localStorage` in SSR mode. Not a v1 risk (no web target), but documented in `apps/mobile/src/features/auth/CLAUDE.md` so future-me does not introduce a web bundle without re-opening this.
- **Slice 15a/15b is the first mobile e2e in the project.** If the local machine lacks a simulator/emulator, the verify command honestly fails (cannot run flow). The plan-reviewer's job is to confirm the implementer has at least one of iOS Simulator or Android Emulator available before starting Slice 15a. Document the prerequisite in the slice's verify section.
- **Test naming convention drift.** Backend uses `test_<unit>_<scenario>` (matches `view-library`'s pattern + `services/api/CLAUDE.md`'s convention). Frontend tests use snake-case-no-prefix names because Jest does not require the `test_` prefix and the project's existing mobile tests follow that style. The mismatch is intentional and per-runtime convention — but it is a place for accidental drift; the plan-reviewer should not flag the mismatch as a bug.
- **Terminology drift.** Spec adds no glossary entries; `Session` stays feature-local (Supabase SDK type). Slices 9a–14b must use the SDK's `Session` type consistently. The `terminology-drift` Stop hook only watches the backend `domain/` folder, so mobile drift is unenforced — manual vigilance only.

Vault citations applied:
- `[vault: wiki/concepts/Test Double.md]` — Slice 2's `InMemoryTokenVerifier` is a Fowler-style **stub** (returns canned responses; not a mock — it does not verify interaction). The distinction matters for Slice 4's consumer test, which asserts behavior given a stubbed verifier, not call patterns.
- `[vault: wiki/concepts/Vertical Slice Architecture.md]` — each mobile slice 9a–14b owns its UI + hook + tests inside `features/auth/`. The one cross-feature import in Slice 14b (`useLibrary`) is test-fixture-only and acceptable.
- `[vault: wiki/concepts/API Gateway Pattern.md]`, `[vault: wiki/concepts/REST.md]`, `[vault: wiki/concepts/Hexagonal Architecture.md]`, `[vault: wiki/concepts/Twelve-Factor App.md]` — already cited in the spec.

## ADR candidates

- **ADR-0006: Supabase Auth + JWKS verification + supersedes ADR-0004.** Written immediately before Slice 3a. Locks in: Supabase Auth as identity provider, altune verifies offline via JWKS (not HS256), 30s symmetric leeway, `iss`/`aud` expectations, email-confirmation-disabled in altune Supabase project for v1, **`@supabase/supabase-js` as the mobile auth client** (folded in; no separate ADR-0007), no `/auth/*` HTTP routes on altune's backend in v1.

No other ADR candidates — the original ADR-0007 (`@supabase/supabase-js`) was folded into ADR-0006 because the mobile client choice is implied by the Supabase decision (per plan-reviewer suggestion). Postgres, FastAPI, hexagonal, react-query, the `expo-secure-store` storage rule, etc. are already governed by ADRs 0001–0005.

## AC coverage map

| AC | Slices |
|----|--------|
| AC#1 (sign-up creates session) | 12 |
| AC#2 (sign-in obtains session) | 11 |
| AC#3 (sign-in failure shows designed error) | 11 |
| AC#4 (session persists across restarts) | 15b (with 15a as harness prerequisite) |
| AC#5 (sign-out clears state + routes to sign-in) | 14a (a + c) + 14b (b) |
| AC#6 (protected routes redirect) | 10 |
| AC#7 (backend 401 on missing/invalid token) | 5 |
| AC#8 (per-user isolation via JWT-derived user_id) | 7 |
| AC#9 (adapter token-verification matrix) | 3a + 3b + 3c |
| AC#10 (`current_user_id` consumer test) | 4 |
| AC#11 (/health stays unauthenticated) | 6 |
| AC#12 (ADR-0004 retirement) | 8 |
| AC#13 (boot-time XOR check) | 1 |

## Commits

Per the project's TDD workflow (`docs/workflows/new-feature.md` step 4), each behavioral slice is **at minimum two commits**:
- `test(auth-integration): add failing test for <behavior>` — RED
- `feat(auth-integration): <summary>` — GREEN
- `refactor(auth-integration): <improvement>` — only if meaningful

ADR-0006 commits as `docs(adr): add ADR-0006 supabase-auth (supersedes ADR-0004)` immediately before Slice 3a. Slice 8 + Slice 15a are cleanup/bootstrap with `[ALLOW-NO-TEST: …]` body markers and ship as single commits.

Rough commit count: 20 slices × ~1.7 commits + 1 ADR + 2 cleanup-single-commits ≈ **34 commits** to ship `auth-integration`. (Original 15-slice plan estimated 24; the splits add ~10 commits — a fair price for the smaller, safer-to-revert slices.)
