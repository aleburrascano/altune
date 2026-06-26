# ADR-0018: OAuth providers (Apple + Google) and the auth deep-link contract

- **Status:** Accepted
- **Date:** 2026-06-25
- **Deciders:** solo + Claude
- **Context tags:** [dependency, pattern, policy]

## Context

The `auth-hardening` spec (`docs/specs/auth-hardening/spec.md`) adds one-tap social sign-in and self-service flows (email confirmation, password reset). All three callbacks — email-confirm, password-recovery, OAuth — return to the app via the `altune://` deep-link scheme, and OAuth needs a native browser-based auth flow. This introduces the first runtime dependencies the auth feature has taken beyond the Supabase SDK, and a deep-link redirect contract the Supabase dashboard must mirror.

## Decision

Add **`expo-web-browser`** (alongside the already-present `expo-linking`) and use Supabase's `signInWithOAuth({ skipBrowserRedirect: true })` to get the provider auth URL, open it with `WebBrowser.openAuthSessionAsync`, and exchange the returned `code` for a session via `exchangeCodeForSession` (reusing the deep-link spine's `parseAuthLink` + `completeAuthIntent`). Ship **Apple + Google together** — App Store Guideline 4.8 requires Sign in with Apple wherever another third-party social login is offered. All auth callbacks ride a **single deep-link handler** (`useAuthDeepLink` + the pure `parseAuthLink`), whitelisting `altune://auth/{confirm,recovery,callback}`.

The browser-based flow covers both providers with one code path and no extra native module. Native one-tap sheets (`expo-apple-authentication`, native Google) are a deferred enhancement, not v1 — so `expo-auth-session` is intentionally **not** taken on.

## Alternatives considered

| Alternative | Why not |
|---|---|
| Google only | Violates Guideline 4.8 on iOS — Apple becomes mandatory the moment any social login ships. |
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
- Apple + Google as a pair on iOS (can't drop Apple while keeping Google). Removing OAuth later means deleting `useOAuth`/`OAuthButtons` and the two deps — low code cost, but provider console cleanup.

## Implementation notes

- Non-code prerequisites (developer/dashboard) tracked in `docs/specs/auth-hardening/plan.md`: register `altune://` redirect URLs; Apple Service ID + key; Google OAuth client; enable both providers in Supabase.
- Deep-link safety: `parseAuthLink` whitelists by path and ignores foreign schemes (`.claude/rules/frontend/rn-security.md`).

## Vault references

- [vault: wiki/concepts/Strategy.md] — `useOAuth` selects provider behind one interface; providers are interchangeable.
- [vault: wiki/concepts/Chain of Responsibility.md] — the single deep-link handler dispatches recognized callbacks, ignores the rest.
- `.claude/rules/design-patterns/structural/adapter.md` — provider identity → Supabase session is an adapter at the boundary.
