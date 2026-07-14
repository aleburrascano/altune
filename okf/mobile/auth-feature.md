---
type: Mobile Feature
title: Auth
description: Supabase-backed sign-in/sign-up/OAuth/password-reset with a single deep-link spine and session-gated routing.
resource: apps/mobile/src/features/auth/
tags: [mobile, feature, auth, supabase, deep-linking, session]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

Owns sign-in, sign-up, sign-out, OAuth (Apple/Google), password reset, and session restoration on the mobile client (ADR-0006). The backend's `auth.RequireUserID` dependency (see [[auth]]) consumes the JWT this feature obtains via Supabase; the mobile app never decodes or manipulates the token itself.

**Session**: `hooks/useSession.ts` subscribes to `supabase.auth.onAuthStateChange` and exposes a discriminated union `SessionState = {status:'loading'}|{status:'signed-in',session}|{status:'signed-out'}` (`types.ts`). `ui/AuthGate.tsx` reads it and redirects: loading → splash, signed-out outside `(auth)` → `/sign-in`, signed-in inside `(auth)` → `/library`. A `useSegments()[0] === '(auth)'` guard prevents an infinite redirect loop, and a `reset-password` route segment is let through regardless of session so a password-recovery session (which is technically "signed-in") doesn't bounce the user off the set-new-password screen.

**Mutations**: `useSignIn`, `useSignUp`, `useResetPassword`, `useUpdatePassword`, `useOAuth` each wrap one Supabase SDK call and return a typed result union (`idle|pending|ok|error`, etc.), never React state fields that could be checked as booleans. Anti-user-enumeration is load-bearing throughout: sign-in collapses all failures to one generic reason, sign-up returns `awaiting-confirmation` identically whether the email is new or already registered, and password-reset always resolves `sent` regardless of whether the address has an account.

**Deep-link spine**: `lib/parseAuthLink.ts` is a pure classifier for `altune://` URLs, whitelisting only `auth/recovery`, `auth/confirm`, `auth/callback` paths (anything else is `ignored`, per `rn-security.md`). `hooks/useAuthDeepLink.ts` subscribes to `Linking` events and calls `completeAuthIntent`, which exchanges `token_hash`/`access_token` or an OAuth `code` for a session via `supabase.auth.verifyOtp`/`setSession`/`exchangeCodeForSession`, then navigates to `/reset-password` for recovery links. `useOAuth.ts` reuses this same `completeAuthIntent` after a native `WebBrowser.openAuthSessionAsync` round-trip — one code path covers both providers.

**Storage/API boundary**: `api/supabaseClient.ts` re-exports the singleton from `@shared/auth/supabaseClient` (promoted there once a second feature needed it); session storage is wired to `expo-secure-store`, never `AsyncStorage`. No file outside `api/`/`types.ts` may import `@supabase/supabase-js` directly.

Key files: `hooks/useSession.ts`, `hooks/useSignIn.ts`, `hooks/useSignUp.ts`, `hooks/useOAuth.ts`, `hooks/useAuthDeepLink.ts`, `lib/parseAuthLink.ts`, `ui/AuthGate.tsx`, `ui/SignInScreen.tsx`.
