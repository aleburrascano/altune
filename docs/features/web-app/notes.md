# Altune on the web — capability note

Status: **shipped, live on prod** (epic #2827, last task #2844 / PR #3010, merge `59bbdb47`,
verified 2026-09-26).

Plan: [`docs/features/web-app/plan.md`](./plan.md).

## What now works

Anyone with an Altune account opens a desktop (or phone) browser, signs in, and uses Altune the
way they use the phone app: search and save music, browse the library and playlists, and hear
it, in a layout built for a wide screen (sidebar, persistent player bar, keyboard and media
keys). A reload keeps them signed in. Same codebase as the phone app — the Expo web export of
`apps/mobile`, no second front end — served same-origin by the existing Caddy, with no go-api
code changes.

## How to reach it

- **Prod:** `https://altune.duckdns.org/` — 200 HTML, strict CSP (see Must-holds), `nosniff`,
  `Referrer-Policy` set. Deep-linkable routes all resolve on a hard reload: `/library`,
  `/library/playlist/<id>`, `/auth/callback`, `/auth/confirm`, `/auth/recovery`, `/sign-in`,
  `/discover`, and more (full list in the plan's Expected signals).
- **Staging:** `https://altune-staging.duckdns.org/` — same shape, own Supabase project
  (the staging Supabase project), Google sign-in intentionally left disabled there (out of scope, see
  plan).
- **API is unchanged:** `/health` (version = deploying commit), `/overseer/`, and `/v1/*` answer
  exactly as before the web deploy — verified on prod: `/health` → `59bbdb47`, `/overseer/` →
  200, `/v1/library` → 401 JSON (unauthenticated, as expected).
- **Auth redirects:** OAuth (Google), signup confirmation, and password recovery use
  `${origin}/auth/callback|confirm|recovery` on web instead of `altune://`. Prod Supabase
  `uri_allow_list = altune://**,https://altune.duckdns.org/auth/**` (additive; every prior
  native entry still present).
- **Release and rollback:** `services/go-api/deploy/RUNBOOK.md` § "Web app". Pipeline:
  `.github/workflows/deploy-web.yml` — `test` → `build-staging` → `deploy-staging` →
  `smoke-staging` → `approve-prod` (GitHub `production` environment, manual gate) →
  `build-prod` → `deploy-prod` → `smoke-prod`. Releases land on the VM at
  `$WEB_ROOT/{staging,prod}/releases/<sha>`, with `current` a relative symlink
  flipped atomically by `web-release.sh <tier> <sha>`; rollback is `web-release.sh <tier>
  <previous-sha>` (no rebuild, no tarball needed). Caddy reads through the symlink per request —
  no reload needed for a web release. Caddy itself is defined in
  `services/go-api/deploy/Caddyfile` (site blocks for both tiers, `handle @web` file-first, else
  falls through to go-api).

## Must-holds it keeps (verified live on prod, 2026-09-26)

1. Every go-api path (`/v1/*`, `/health`, `/test/*`, `/admin/*`, `/overseer/*`) answers from
   go-api/Overseer, never the web export — confirmed: `/health` returns go-api's version JSON,
   `/overseer/` 200s, `/v1/library` 401s.
2. Web directory empty/missing degrades to today's behaviour (API never goes down with web) —
   Caddy's file-first/API-fallback rule; `deploy/caddy-routing_test.sh` proves it against the
   real Caddy image for both site blocks.
3. No expo-router route starts with `v1`, `health`, `test`, `admin`, `operator`, or `overseer`
   (checked by test).
4. A signed-in web user survives a reload; sign-out clears `localStorage` of any Supabase
   session.
5. A hard reload on any deep link (`/library`, `/library/playlist/<id>`, `/player`, etc.) renders
   that screen, not a 404 or the API's response — the dynamic `/library/playlist/[id]` rewrite
   is in place on both Caddy blocks.
6. CSP `script-src` has no `'unsafe-inline'`, no `'unsafe-eval'` — only `'self'` plus the pinned
   `sha256-…` hash of the export's one inlined hydration script. `build-staging`/`build-prod`
   fail the pipeline if the export's inline script hash isn't in the CSP.
7. A track paused past the presign TTL resumes at the same position on play (re-presigns
   silently).
8. Web playback calls only `POST /v1/audio-urls` for sources, never `GET
   /v1/tracks/{id}/audio`.
9. Offline pin/download controls are not rendered on web.
10. Native build unchanged: `trackPlayerProvider` still selected on iOS/Android, secure-store
    still the native session store.
11. Supabase redirect allow-list after the change is a strict superset of before (no native
    `altune://` entry dropped) — confirmed on prod:
    `altune://**,https://altune.duckdns.org/auth/**`.

## Not yet user-proven

Google sign-in and playback on prod are shipped and should work per the must-holds above, but
have not yet been exercised by a real user on prod — the operator will try both directly.

## Known follow-up (already ticketed, not a blocker)

**#3075** — `compose.prod.yml` mounts the Caddyfile as a single file
(`./Caddyfile:/etc/caddy/Caddyfile:ro`); a `git reset --hard` deploy swaps the inode but the
running container keeps the old one, so a plain `caddy reload` can silently serve a stale
config. Hit once while shipping this epic (the playlist deep-link rewrite didn't go live on
staging until a manual `docker restart altune-caddy`); fixed for that instance but the mount
itself is unfixed. Anyone changing `Caddyfile` next should check the container's Caddyfile hash
against the deployed commit, per #3075's fix directions, until it's fixed for good.

## Deliberately out of scope (see plan for the full list and reasoning)

Offline downloads/pinning in the browser, cookie-based server-held sessions, PWA/service worker,
a desktop native wrapper, and Google sign-in on staging (no OAuth client there; proven on prod
instead).
