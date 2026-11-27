# Web session storage

The web app keeps the whole Supabase session, refresh token included, in `localStorage` (`client/src/mobile/authStorage.web.ts`). Native keeps it in the Keychain (`client/src/mobile/authStorage.ts`, `SecureStore`). A script that runs on the page can read the web session, so this is an accepted risk, not a safe default. The alternative is a cookie set by a backend-for-frontend (BFF), which keeps the refresh token out of script reach but brings CSRF into scope and needs a server-side session; it is not planned.

## What limits the risk

| Control | Where | What keeps it true |
|---|---|---|
| Strict CSP: `script-src 'self'` plus one inline-script hash, `object-src 'none'`, `frame-ancestors 'none'`, `connect-src` limited to the API origin and the Supabase project | `services/go-api/deploy/Caddyfile` (the `Content-Security-Policy` lines of the prod and staging sites) | `scripts/ci/deploy-web-csp-check.sh` (run by `scripts/ci/web-build.sh`) fails a web build whose inline script has no hash in the CSP; nothing yet rejects an `unsafe-inline` added by hand, so review holds that line |
| Access token lives one hour (`jwt_exp` 3600) | `services/supabase/auth/base.json` | the `supabase-auth.sh diff` drift check in `staging-sync.yml` |
| Refresh-token rotation on, 10 s reuse window, so a stolen token that is used after the real one is detected | `services/supabase/auth/base.json` (`refresh_token_rotation_enabled`, `security_refresh_token_reuse_interval`) | the same drift check |

## What does not limit it

`sessions_timebox` and `sessions_inactivity_timeout` are 0 in `services/supabase/auth/base.json`, so a refresh token does not expire by age or by idleness. A stolen session therefore lasts until the user signs out or the token is rotated by use. Setting a time-box or inactivity timeout is a Supabase plan feature and a production auth change, so it is a decision for the owner, not something this page assumes.

## Rules for code

- The session is written only through `authStorage.web.ts`; no other web code reads or writes the Supabase keys.
- No `unsafe-inline` or `unsafe-eval` in `script-src`, and no third-party script origin, because the CSP is the main control.
- A change to storage (a cookie, an in-memory store) updates this page in the same PR.
