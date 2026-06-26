# ADR-0018: OAuth providers (Apple + Google) and the auth deep-link contract

- **Status:** Accepted
- **Date:** 2026-06-25
- **Deciders:** solo + Claude
- **Context tags:** [dependency, pattern, policy]

## Context

The `auth-hardening` spec (`docs/specs/auth-hardening/spec.md`) adds one-tap social sign-in and self-service flows (email confirmation, password reset). All three callbacks — email-confirm, password-recovery, OAuth — return to the app via the `altune://` deep-link scheme, and OAuth needs a native browser-based auth flow. This introduces the first runtime dependencies the auth feature has taken beyond the Supabase SDK, and a deep-link redirect contract the Supabase dashboard must mirror.

## Decision

Add **`expo-web-browser`** (alongside the already-present `expo-linking`) and use Supabase's `signInWithOAuth({ skipBrowserRedirect: true })` to get the provider auth URL, open it with `WebBrowser.openAuthSessionAsync`, and exchange the returned `code` for a session via `exchangeCodeForSession` (reusing the deep-link spine's `parseAuthLink` + `completeAuthIntent`). All auth callbacks ride a **single deep-link handler** (`useAuthDeepLink` + the pure `parseAuthLink`), whitelisting `altune://auth/{confirm,recovery,callback}`.

**Ship Google only for now; defer Sign in with Apple.** Apple sign-in requires a paid Apple Developer account ($99/yr), which the project does not currently have. App Store Guideline 4.8 (Apple required wherever another social login is offered) only applies to App Store submissions — and submitting at all needs the same paid account — so with no App Store distribution, 4.8 cannot trigger and Google-only is compliant. `useOAuth` stays provider-agnostic (it already accepts `'apple'`), so adding Apple is one button + the dashboard wiring if the account/App-Store path ever opens.

The browser-based flow covers Google with one code path and no extra native module. Native one-tap sheets (`expo-apple-authentication`, native Google) are a deferred enhancement — so `expo-auth-session` is intentionally **not** taken on.

## Alternatives considered

| Alternative | Why not |
|---|---|
| Apple + Google together (original plan) | Apple needs a paid developer account the project won't take on; 4.8 is moot without App Store distribution, so the extra setup buys nothing now. Revisit if that changes. |
| Passwordless / magic-link instead of OAuth | A different product direction (parked in `docs/ideation/2026-06-25-auth-feature-ideation.md`); the user explicitly chose OAuth. |
| `expo-auth-session` for manual OAuth/PKCE | Supabase's `signInWithOAuth` + `WebBrowser` already handles the browser flow for both providers; pulling in a second auth library is an unused dependency. |
| A separate handler per callback type | Three handlers duplicate URL parsing + validation; one whitelisted spine is simpler and safer. |

## Consequences

### What becomes easier
- One-tap sign-in; email-confirm and password-reset reuse the same deep-link spine.
- Provider set is swappable behind `useOAuth` (Strategy) without touching screens.

### What becomes harder
- A new native dep (`expo-web-browser`) → OAuth cannot run in Expo Go reliably; verification needs a dev build.
- A redirect-URL contract now spans app + Supabase dashboard + Apple/Google consoles; drift breaks callbacks silently.

### What we're committing to (and the cost to reverse)
- Google-only social sign-in. If the app is ever submitted to the App Store with Google sign-in present, Guideline 4.8 forces adding Apple (paid account) or hiding Google on iOS — a known future fork, cheap in code (`useOAuth` is already provider-agnostic).

## Implementation notes

- Non-code prerequisites (developer/dashboard) tracked in `docs/specs/auth-hardening/plan.md`: register `altune://` redirect URLs; Apple Service ID + key; Google OAuth client; enable both providers in Supabase.
- Deep-link safety: `parseAuthLink` whitelists by path and ignores foreign schemes (`.claude/rules/frontend/rn-security.md`).

## Vault references

- [vault: wiki/concepts/Strategy.md] — `useOAuth` selects provider behind one interface; providers are interchangeable.
- [vault: wiki/concepts/Chain of Responsibility.md] — the single deep-link handler dispatches recognized callbacks, ignores the rest.
- `.claude/rules/design-patterns/structural/adapter.md` — provider identity → Supabase session is an adapter at the boundary.
