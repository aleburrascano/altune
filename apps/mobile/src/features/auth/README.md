# auth — seam map

Email/password and Google OAuth on top of Supabase GoTrue. The session itself lives in
`@shared/auth` (`supabaseClient`, `useSession`, `useSignOut`, `sessionExpired`, `signOutCleanup`);
this folder is what sits on top of it — the screens, the `altune://` links that carry credentials,
and the gate that decides which screen you get. Entry points, all mounted from
`src/app/_layout.tsx`:

- `ui/AuthGate.tsx` — wraps the whole app; renders splash, a redirect, a notice, or the children.
- `hooks/useAuthDeepLink.ts` — mounted as `AuthDeepLinkBridge` _inside_ the gate; the global
  `altune://` listener (initial URL + every later one).
- `ui/TestAuthBridge.tsx` — mounted _outside_ the gate (see §1); the non-production test-auth
  bootstrap.

## 1. Seams

Four invariants that no single file makes visible, because each one only holds across files.
Read these before changing anything below.

- **One redirect, two consumers** — `completeAuthIntent.ts`, `hooks/useOAuth.ts`,
  `hooks/useAuthDeepLink.ts`. A single OAuth redirect (`altune://auth/callback`) is delivered
  twice: once as `openAuthSessionAsync`'s result to the hook that opened the browser, once to the
  global `Linking` listener. Both call `completeAuthIntent`, and the credential they carry is
  single-use, so the loser's exchange would fail server-side. `completeAuthIntent` keeps the last
  credential it started consuming (`code` / `token_hash` / `access_token`) and claims it
  **synchronously, before the first await**, so the second delivery returns `deduped` instead of
  racing the exchange. `useOAuth` therefore treats `deduped` as success — the session exists, the
  other listener established it (#659).
- **The recovery-unlock window** — `recoveryUnlock.ts`, `completeAuthIntent.ts`, `ui/AuthGate.tsx`,
  `ui/SetNewPasswordScreen.tsx`. The `reset-password` route is reachable from the bare `altune`
  scheme, so the route segment proves nothing; the gate renders the password form only while an
  in-memory unlock window is open, and `ui/InvalidRecoveryLinkNotice.tsx` otherwise. Who touches
  the window, and only these:
  - `completeAuthIntent.ts` opens it (`markRecoveryUnlocked`) after a _recovery_ link's
    `verifyOtp`/`setSession` actually succeeded, then `router.replace('/reset-password')` — never
    on reaching the route, never on a failed verification.
  - `recoveryUnlock.ts` holds it as an absolute deadline, `RECOVERY_UNLOCK_WINDOW_MS` (5 min) wide,
    so a marker left by an abandoned flow is not exploitable later.
  - `ui/AuthGate.tsx` reads it through `useRecoveryUnlocked` (a `useSyncExternalStore`
    subscription, so expiry and clearing re-render the gate).
  - `ui/SetNewPasswordScreen.tsx` closes it (`clearRecoveryUnlock`) once the password update
    resolves `ok`, so the screen cannot be re-entered without a fresh recovery link (#656).
- **PKCE only on the OAuth callback** — `completeAuthIntent.ts`. `exchangeOAuth` refuses an
  `auth/callback` link that carries no `code`: a captured implicit-grant redirect with an inline
  access/refresh pair is a `failure`, never a `setSession`, because a token pair intercepted off
  the bare `altune` scheme could otherwise be replayed (#655). The refusal is specific to the OAuth
  path — `auth/recovery` and `auth/confirm` links legitimately fall back to `setSessionFrom`, which
  is why the unlock window above, not the link shape, is what guards the password form.
- **The failure lockout outlives the screen** — `attemptLockout.ts`, `hooks/useSignIn.ts`,
  `hooks/useResetPassword.ts`. Both hooks wrap their SDK call in `lockoutOnRepeatedFailure`, which
  refuses the account in the call's first argument once a run of failures reaches
  `LOCKOUT_AFTER_FAILURES` and returns `too_many_attempts` without touching the network (#1640).
  The runs live in the module, not in a hook, precisely so stepping to "forgot password" and back
  does not hand out a fresh allowance; they are keyed on the trimmed, lower-cased address so
  neither a capital nor a space does either, and keyed **per address** so one account's lockout can
  never bar the rest. Nothing in the run is derived from what Supabase answered, so it tells a
  stranger nothing about which addresses exist. It is memory-only and resets with the app — depth
  behind GoTrue's own throttle, not a replacement for it.
- **`TestAuthBridge` mounts outside `AuthGate`** — `ui/TestAuthBridge.tsx`, `src/app/_layout.tsx`.
  The bridge signs the test user in, so it runs while signed-out; the gate redirects away from its
  children before they mount, so a bridge placed inside it would never run. This is a precondition
  on `_layout.tsx`'s mount order that the JSX itself does not state. `AuthDeepLinkBridge` sits
  inside the gate, by contrast, and only runs where the gate renders its children.

## 2. Link plumbing

The path from an `altune://` URL to a session. No UI, no React.

- `parseAuthLink.ts` — an external, unverified link parsed on the JS thread: caps length
  (4096) and param pairs (64) before walking them, reads query _and_ fragment, and maps only the
  three known paths (`auth/recovery`, `auth/confirm`, `auth/callback`) to an intent, everything
  else to `ignored`. Owns the redirect URLs handed to Supabase (`OAUTH_REDIRECT_URL`,
  `CONFIRM_REDIRECT_URL`, `RECOVERY_REDIRECT_URL`) so they round-trip through the same vocabulary.
- `completeAuthIntent.ts` — consumes an intent and reports `success` / `failure` / `deduped` /
  `ignored`, so a caller can tell the user the truth instead of assuming the exchange worked. Takes
  the router it navigates with as an argument; both callers pass `useRouter()`.
- `recoveryUnlock.ts` — the unlock window of §1, as a tiny external store.

## 3. Hooks (`hooks/`)

- `useAsyncAuthAction.ts` — the idle/pending/terminal envelope every action hook shares, bounded by
  `authDeadline.ts` so a Supabase call that never resolves lands on a terminal `network` error
  instead of pinning the submit button at `pending`.
- `useSignIn.ts`, `useSignUp.ts`, `useResetPassword.ts`, `useUpdatePassword.ts` — one SDK call
  each, mapping the resolved `{ error }` to a typed `reason` via `supabaseAuthError.ts`.
- `useOAuth.ts` — opens the provider in the in-app browser and consumes the redirect it wins (§1).
  It runs three legs rather than one SDK call, so it bounds them itself: the two network legs on
  the shared 20 s budget, the browser leg on `OAUTH_BROWSER_TIMEOUT_MS` (5 min), which is paced by
  a human at the provider. Its terminal state is dropped if the screen unmounted meanwhile (#1642).
- `useAuthDeepLink.ts` — the global listener; it has no UI to report to, so a rejected exchange is
  swallowed rather than left as an unhandled rejection.

Pure helpers the hooks and UI consume: `authDeadline.ts` (`AUTH_ACTION_TIMEOUT_MS` and the race
that abandons a stalled leg as a `network` failure — one owner, so the budget cannot drift per
hook), `supabaseAuthError.ts` (classifies a resolved Supabase error by shape — transport, weak
password, already registered), `errorCopy.ts` (reason → user-facing text), `validation.ts` (email
and password rules), `attemptLockout.ts` (the per-address failure lockout of §1).

## 4. Presentation (`ui/`)

Screens: `SignInScreen`, `SignUpScreen`, `ForgotPasswordScreen`, `SetNewPasswordScreen` (the
routes under `src/app/(auth)/` and `src/app/reset-password.tsx`), plus `CheckEmailNotice`, which
`SignUpScreen` renders in place of itself once sign-up is awaiting confirmation. All sit on the
`hero/` layout and are built from `AuthForm` (sign-in/sign-up), `OAuthButtons`, `AuthErrorBanner`
and `BackToSignInLink`.

Full-screen interruptions, all on `AuthFullScreenNotice`: `InvalidRecoveryLinkNotice` (§1),
`SessionExpiredNotice`, and the gate's own splash.
