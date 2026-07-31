# shared/auth — router

The Supabase client singleton plus the three pieces of session state that outlive any one feature: the session hook, the session-expired signal, and sign-out.

Layout:

- `supabaseClient.ts` — the app's single `createClient` call, its `expo-secure-store` keychain adapter and the `localStorage` web fallback.
- `useSession.ts` — `useSession`, the `SessionState` union, and `forgetPreviousUsersLocalData`.
- `sessionExpired.ts` — `markSessionExpired`, `clearSessionExpired`, `getSessionExpired`, `useSessionExpired`, `_listenerCountForTest`.
- `useSignOut.ts` — `useSignOut` and the `SignOutResult` union.

Dependencies: `@supabase/supabase-js`, `expo-secure-store`, `@tanstack/react-query`, `@shared/offline/pinnedStore`. Consumed by `features/auth`, `features/settings`, `shared/api-client` and `shared/events`.

## Rules

- Keep `createClient` to this one call site; every other file takes the `supabase` singleton.
- Never value-import `@supabase/supabase-js` outside `supabaseClient.ts` — `import type` is fine anywhere.
- Never decode, parse or inspect the access token here; it is opaque and the backend verifies it.
- Persist the session through `expo-secure-store`, never `AsyncStorage`.
- Pass `KEYCHAIN_OPTS` to all three keychain operations — an asymmetric option set orphans the item.
- Treat a session without a `user` object as no session; every consumer reads through `session.user`.
- Clear local data on an identity *change* only, never on every auth event.
- Keep `markSessionExpired` a signal, not an action — never sign the user out from a 401.
- Clear the query cache on sign-out whether the SDK call succeeds, errors or throws.

Tests: `__tests__/` — `supabaseClient`, `sessionExpired` (`.ts` + `.tsx`), `useSession`, `useSession.property`, `useSignOut`, `authContract`, `acceptance`, `slice-invariants`. Categories and rejections: `okf/testing/shared-auth.md`.

Knowledge base: `okf/mobile/shared-auth.md` — read before structural work; update in the same commit when behavior it describes changes (pre-commit hook enforces).
