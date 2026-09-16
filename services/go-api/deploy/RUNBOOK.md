# Deploy runbook

Operational deploy steps for the OCI prod VM (`altune.duckdns.org`, repo at
`/home/ubuntu/altune`). This lives next to the deploy machinery on purpose: the
release chair (`ship`) reads it here; no other context needs it.

There is **no staging tier**. Merge to `main` auto-deploys `go-api` to prod.

## go-api (automated)

CI handles it: `.github/workflows/deploy-backend.yml` triggers on
`services/go-api/**` pushes to `main`, SSHes in, and runs
`services/go-api/deploy/blue-green.sh` (builds + flips `go-api-blue`/`go-api-green`).
Nothing manual.

## overseer (automated)

CI handles it too: `.github/workflows/deploy-backend.yml` also triggers on
`services/overseer/**` pushes to `main`, and the SSH deploy step runs
`services/go-api/deploy/overseer.sh` right after `blue-green.sh`. That script
builds the image and `up -d overseer` (a single in-memory container, no blue-green
swap — a brief `/overseer` blip, go-api traffic untouched), then:

- **chowns the data volume** to uid 1000 if a pre-fix volume is still `root:root`,
  and force-recreates, so overseer can write its token file (`#1471`);
- **self-verifies**: waits one collect cycle, then fails the deploy if overseer is
  not `healthy` or its logs show operator-token persistence/seed breakage (a
  generic `collect.failed`, e.g. the OCI-usage 404 `#1487`, does **not** fail it).

The env check runs before the container is touched, so a missing required
`OVERSEER_*` var fails the deploy loudly instead of crash-looping in prod.
Nothing manual. Full detail: `docs/features/overseer/deploy.md`.

Caddy already routes `/overseer/*` → `altune-overseer:8090` (`deploy/Caddyfile`),
unchanged. overseer is in-memory only — no DB migration, so rollback is just
redeploying the prior ref.

### Required env (`services/go-api/.env.production`)

The new binary **fails closed / crash-loops** without these:

- `OVERSEER_OWNER_USER_ID` — the owner's Supabase user id (UUID). The allowlist.
- `OVERSEER_SUPABASE_URL`, `OVERSEER_SUPABASE_ANON_KEY` — public; also served to
  the SPA at `/config.json` so it can init supabase-js for login.
- `OVERSEER_GOAPI_URL` — go-api base the buckets read.
- `OVERSEER_GOAPI_REFRESH_TOKEN` — operator (== owner) Supabase refresh token
  used to call go-api. This is only the **first-boot seed**: once overseer runs it
  rotates the token and persists the live one to the `overseer-data` volume
  (`/var/lib/overseer/refresh_token`, chmod 600), so restarts resume the chain. You
  only touch this env var on the very first deploy, or to recover after the volume
  is wiped.
- `OVERSEER_BASE_PATH=/overseer`, `OVERSEER_OCI_ENABLED` (cost bucket).
- **Not** `OVERSEER_OWNER_TOKEN` — retired with the old cookie dashboard.

### Refresh-token rotation (persisted — no manual reseed)

Supabase rotates the operator refresh token on every use. Overseer persists the
rotated token to the `overseer-data` volume (`/var/lib/overseer/refresh_token`), so
a restart resumes the live chain instead of replaying the spent seed. **No manual
reseed before a restart.** Just `up -d overseer`.

Recover a fresh seed **only** if the volume is wiped or the chain is truly lost
(every bucket `source_down` with `status 400` right after a *clean-volume* start):

1. Incognito window → `https://altune.duckdns.org/overseer/` → sign in.
2. DevTools Console:
   ```js
   (() => { for (const s of [localStorage, sessionStorage]) for (const k of Object.keys(s)) { try { const v = JSON.parse(s.getItem(k)); const rt = v?.refresh_token || v?.currentSession?.refresh_token; if (rt) return rt; } catch(e){} } return 'NOT FOUND'; })()
   ```
3. Put that value in `OVERSEER_GOAPI_REFRESH_TOKEN`, remove the stale persisted file
   (`docker compose -f deploy/compose.prod.yml exec overseer rm -f /var/lib/overseer/refresh_token`,
   or `docker volume rm go-api_overseer-data` while the container is down), then
   `up -d overseer`. A persisted file always wins over the env seed, so the seed is
   ignored until the file is gone.
4. Close the incognito window (so its session doesn't rotate the token out from
   under overseer). **Do not** curl-exchange the token to "test" it first — that
   spends it.

### Smoke test (after any overseer deploy)

`overseer.sh` already self-verifies health + token/persist in-pipeline; this is the
extra edge/browser cross-check you run by hand when you want end-to-end proof:

```bash
curl -s -o /dev/null -w '%{http_code}\n' https://altune.duckdns.org/overseer/           # 200 (SPA)
curl -s https://altune.duckdns.org/overseer/config.json                                 # public supabase config
curl -s -o /dev/null -w '%{http_code}\n' https://altune.duckdns.org/overseer/api/buckets # 401 (guarded)
curl -s -o /dev/null -w '%{http_code}\n' https://altune.duckdns.org/overseer/health      # 200
# then on the VM, confirm buckets collect (want 0):
docker compose -f deploy/compose.prod.yml logs --since=25s overseer | grep -c collect.failed
```

Finally, sign in at `/overseer/` and confirm the buckets show live data (not
`source_down`). Owner login is verified against Supabase's keys (JWKS), so only a
real browser login proves it end to end.
