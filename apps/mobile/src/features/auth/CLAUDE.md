# auth — feature-local router

Per ADR-0006: sign-in / sign-up / sign-out and session restoration on the mobile client. The backend's `current_user_id` dependency consumes the JWT this feature obtains via Supabase.

The singleton SDK client, `useSession` (+ `SessionState`), `useSignOut` and the session-expired store live in `shared/auth/` — promoted because 2+ features consume them. This feature owns the *flows*:

- `hooks/` — `useSignIn`, `useSignUp`, `useResetPassword`, `useUpdatePassword`, `useOAuth`, `useAuthDeepLink`: thin SDK wrappers surfacing a typed result union.
- `lib/parseAuthLink.ts` + `lib/completeAuthIntent.ts` — the deep-link spine: pure classifier plus token-exchange/routing.
- `lib/validation.ts`, `lib/errorCopy.ts`, `lib/isNetworkError.ts` — form policy, copy, transport-failure classifier.
- `ui/` — `AuthGate`, `AuthForm`, the screens, and the `hero/` visuals.

## Rules

- Never value-import `@supabase/supabase-js` — the singleton comes from `@shared/auth/supabaseClient`. `import type` is fine.
- Never decode or manipulate the access token here; the session is opaque and the backend verifies.
- Never assert the exact error wording in tests — pin `testID="auth-error"` only.
- Import `@shared/ui` primitives directly by path, not through the barrel.

Tests: none yet — this slice's suite was reset on 2026-07-30 and is rebuilt per `okf/playbooks/test-taxonomy.md`, with the per-category verdict committed to `okf/testing/<slice>.md`.
Why each rule exists, plus the web/SSR caveat and the e2e setup: `okf/mobile/auth-feature.md` — read before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).
