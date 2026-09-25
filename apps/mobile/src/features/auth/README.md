# auth — seam map

Email/password and Google OAuth on top of Supabase GoTrue. The session itself lives in
`@shared/auth` (`supabaseClient`, `useSession`, `useSignOut`, `sessionExpired`; `signOutCleanup` lives in `@shared/session`);
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
  single-use, so the loser's exchange would fail server-side. `completeAuthIntent` keeps the
  credential it is consuming (`code` / `token_hash` — only a shape it can actually spend) **and the
  promise consuming it**, claimed **synchronously, before the first await**, so the second delivery
  awaits that same exchange instead of racing it. It reports `deduped` only once the winner's
  exchange actually succeeded, and the winner's own `failure` otherwise — `useOAuth` treats
  `deduped` as success, so a synthesized one would report a session nobody has (#659, #1641). For
  the same reason the claim is released when the exchange fails: the link in the inbox is still
  live, and a permanent claim would answer every later tap `deduped` (#1641). Which is also why a
  refused shape claims nothing at all: `deduped` asserts a session exists, so a refusal must never
  earn it (#1637).
- **The recovery-unlock window** — `recoveryUnlock.ts`, `completeAuthIntent.ts`, `ui/AuthGate.tsx`,
  `ui/SetNewPasswordScreen.tsx`, `shared/session/signOutCleanup.ts`. The `reset-password` route is
  reachable from the bare `altune` scheme, so the route segment proves nothing; the gate renders
  the password form only while an unlock window is open **for the account currently signed in**,
  and `ui/InvalidRecoveryLinkNotice.tsx` otherwise. Who touches the window, and only these:
  - `completeAuthIntent.ts` opens it (`markRecoveryUnlocked`) after a _recovery_ link's `verifyOtp`
    actually succeeded, then `router.replace('/reset-password')` — never on reaching the route,
    never on a failed verification. It binds the window to the user id the server named on that
    verification, and fails the link closed if the verification named none (#1638).
  - `recoveryUnlock.ts` holds `{ userId, openedAt, openedTick }` on two clocks, with a window `RECOVERY_UNLOCK_WINDOW_MS`
    (5 min) wide, so a window left by an abandoned flow is exploitable neither later nor by anyone
    else. It registers `clearRecoveryUnlock` with `onSignOut`, so every identity change — a
    sign-out, or a switch straight into another account — closes it; that registration is how both
    `forgetPreviousUsersLocalData` call sites reach it without `@shared` importing a feature.
  - `ui/AuthGate.tsx` reads it through `useRecoveryUnlocked(signedInUserId)` (a
    `useSyncExternalStore` subscription, so expiry and clearing re-render the gate), which answers
    `false` for any other account and for signed-out.
  - `ui/SetNewPasswordScreen.tsx` closes it (`clearRecoveryUnlock`) once the password update
    resolves `ok`, so the screen cannot be re-entered without a fresh recovery link (#656).
- **No link becomes a session on its own word** — `completeAuthIntent.ts`, `@shared/auth/supabaseClient.ts`.
  Every path spends its credential against the server: `auth/callback` an `exchangeCodeForSession`
  on a PKCE `code`, `auth/recovery` and `auth/confirm` a `verifyOtp` on a `token_hash` their own
  path may spend. A captured implicit-grant link with an inline access/refresh pair is a `failure`
  on all three, because a token pair intercepted off the bare, unverified `altune` scheme is
  replayable indefinitely while a `code` is worthless without the verifier we hold (#655, #1637).
  The rule is structural, not a check to remember: the `AuthClient` slice this module accepts has
  no `setSession` on it, so there is no bare-token branch to fall back to.
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
  The scheme itself is read from the Expo config (`Constants.expoConfig.scheme`, i.e. `app.json`)
  rather than restated, and a build without one throws at import instead of ignoring every link.
- `completeAuthIntent.ts` — consumes an intent and reports `success` / `failure` / `deduped` /
  `ignored`, so a caller can tell the user the truth instead of assuming the exchange worked. Takes
  the router it navigates with as an argument; both callers pass `useRouter()`. Every `failure`
  names its `cause`, and a `gotrue_rejected` one carries the server's `error` — an expired link, an
  unreachable GoTrue and a template composing a type this path may not spend used to be the same
  bare `failure`, which is the one distinction a support ticket needs and no reproduction can
  recover once the link is spent (#1647).
- `errorDetail.ts` — the one place a failure's diagnostic detail is copied out of a thrown value or
  a Supabase `{ error }`. Redaction by projection: only `name` / `message` / `code` / `status` exist
  on the other side, so a credential hanging off an SDK error cannot ride into a log, and a
  non-Error is reported by type rather than stringified.
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
- `useAuthDeepLink.ts` — the global listener; it has no UI to report to, so a link that dies here
  is absorbed rather than left as an unhandled rejection — but `console.warn`ed first, with the
  intent kind and the failure's cause, never `intent.params`. A user who taps a confirm or reset
  link and sees nothing happen is exactly the support ticket this log is the only trace of (#1647).

Pure helpers the hooks and UI consume: `authDeadline.ts` (`AUTH_ACTION_TIMEOUT_MS` and the race
that abandons a stalled leg as a `network` failure — one owner, so the budget cannot drift per
hook), `supabaseAuthError.ts` (classifies a resolved Supabase error by shape — transport, weak
password, already registered, invalid credentials, unconfirmed email), `errorReason.ts` (the
`AuthErrorReason` taxonomy and its user-facing text), `errorDetail.ts` (§2 — the diagnostic detail
a failure may keep, as opposed to the reason it may show), `validation.ts` (email and password
rules), `attemptLockout.ts` (the per-address failure lockout of §1).

`useAsyncAuthAction` logs the `unknown` branch and nothing else: every other reason names its own
cause, while `unknown` is the state with nothing behind it, and the terminal state's shape belongs
to the calling hook so there is nowhere on it to put one (#1647).

Each classifier recognises its reason **positively**, by GoTrue's own code, and an error matching
none of them is `unknown`: an unnamed rejection refuses the request, it does not rule on the
password, so its copy may not say the password was wrong. That is why the accusing words now hang
off the `invalid_credentials` reason in `errorReason.ts` rather than off the sign-in screen's
fallback string, which every unrecognised failure — an unconfirmed address, a rate limit below
429 — used to inherit (#1646).

The same rule governs the one signal that arrives without an error: `useSignUp` reads "already
registered" from a success whose `user.identities` is an _empty array_ — GoTrue's anti-enumeration
behaviour, which no version of `AuthResponse` promises — so it is read only once it is actually an
array, and a response that stops carrying it is `unknown` rather than a coin flip between that and
"check your inbox" (#1650).

## 4. Presentation (`ui/`)

Screens: `SignInScreen`, `SignUpScreen`, `ForgotPasswordScreen`, `SetNewPasswordScreen` (the
routes under `src/app/(auth)/` and `src/app/reset-password.tsx`), plus `CheckEmailNotice`, which
`SignUpScreen` renders in place of itself once sign-up is awaiting confirmation. All sit on the
`hero/` layout and are built from `AuthForm` (sign-in/sign-up), `OAuthButtons`, `AuthErrorBanner`
and `BackToSignInLink`.

Full-screen interruptions, all on `AuthFullScreenNotice`: `InvalidRecoveryLinkNotice` (§1),
`SessionExpiredNotice`, and the gate's own splash.
