# Overseer deploy

How the Overseer container reaches the OCI prod VM (`altune.duckdns.org`, repo at
`/home/ubuntu/altune`). Companion to the release runbook at
`services/go-api/deploy/RUNBOOK.md`; keep the two in sync.

There is **no staging tier**. Merge to `main` auto-deploys straight to prod.

## Automated (since #1470)

Overseer ships on merge, like go-api. `.github/workflows/deploy-backend.yml`
triggers on pushes to `main` under `services/go-api/**` **or**
`services/overseer/**`, runs the go-api and overseer test suites as prerequisites,
then in one SSH step fast-forwards `main` and deploys **both** services:

1. `services/go-api/deploy/blue-green.sh` — go-api's blue-green swap.
2. `services/go-api/deploy/overseer.sh` — overseer's rebuild + recreate.

Both run on every deploy, whichever side the push touched. Overseer is a single
in-memory container with no go-api dependency, so it gets no blue-green: `overseer.sh`
rebuilds the image and recreates the one container. That is a brief `/overseer`
restart blip; go-api traffic is unaffected (blue-green only moves the `go-api-*`
Caddy upstream). Caddy routes `/overseer/*` → `altune-overseer:8090`
(`services/go-api/deploy/Caddyfile`), unchanged.

Overseer is in-memory only — no DB, no migration — so rollback is redeploying the
prior ref.

### Fail-fast env check

`overseer.sh` verifies `services/go-api/.env.production` before touching the
container. A missing or empty required var fails the deploy loudly here instead of
letting the new binary crash-loop in prod (the container's `config.validate()`
fails closed on the same three vars). Required:

- `OVERSEER_OWNER_USER_ID` — the owner's Supabase user id (UUID). The allowlist.
- `OVERSEER_SUPABASE_URL`, `OVERSEER_SUPABASE_ANON_KEY` — public; also served to the
  SPA at `/config.json` so it can init supabase-js for login. The URL is also the
  token binding: the verifier accepts only tokens whose `iss` is
  `{OVERSEER_SUPABASE_URL}/auth/v1` with audience `authenticated`, so pointing it at
  anything other than the host GoTrue stamps on its tokens (a custom auth domain,
  say) rejects every login with a 401.

Other overseer vars are read by the app but not gated here (the app degrades a
bucket to `source_down` rather than crash-looping):

- `OVERSEER_GOAPI_URL` — go-api base the buckets read.
- `OVERSEER_GOAPI_REFRESH_TOKEN` — operator (== owner) Supabase refresh token used
  to call go-api. **See the gotcha below.**
- `OVERSEER_BASE_PATH=/overseer`, `OVERSEER_OCI_ENABLED` (cost bucket).
- **Not** `OVERSEER_OWNER_TOKEN` — retired with the old cookie dashboard.

### GOTCHA: the operator refresh token is single-use and rotates

Supabase rotates refresh tokens on every use. The running container holds the
rotated token **in memory only**, so **a restart throws the chain away** and falls
back to the seed in `.env.production` — which by then is spent (`status 400` → every
go-api bucket shows `source_down`, dashboard still serves). Since the auto-deploy
recreates the container on every deploy, it restarts overseer.

So **before a deploy that will restart overseer, seed a FRESH refresh token**:

1. Incognito window → `https://altune.duckdns.org/overseer/` → sign in.
2. DevTools Console:
   ```js
   (() => { for (const s of [localStorage, sessionStorage]) for (const k of Object.keys(s)) { try { const v = JSON.parse(s.getItem(k)); const rt = v?.refresh_token || v?.currentSession?.refresh_token; if (rt) return rt; } catch(e){} } return 'NOT FOUND'; })()
   ```
3. Put that value in `OVERSEER_GOAPI_REFRESH_TOKEN` on the VM, then let the deploy
   run (or `up -d overseer` for a manual redeploy).
4. Close the incognito window (so its session doesn't rotate the token out from
   under overseer). **Do not** curl-exchange the token to "test" it first — that
   spends it.

(#1471 will persist rotated tokens to a volume so restarts resume cleanly and this
step goes away.)

## Manual fallback

If CI is down, promote overseer by hand on the VM:

```bash
ssh -i ~/.ssh/altune-prod.key ubuntu@altune.duckdns.org
cd /home/ubuntu/altune && git fetch origin && git checkout main && git reset --hard origin/main
cd services/go-api
bash deploy/overseer.sh   # same env check + build + recreate CI runs
```

## Smoke test (after any overseer deploy)

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

## Verifying the wiring changed

`overseer.sh` has a self-test, `services/go-api/deploy/overseer_test.sh` (same shape
as `blue-green_test.sh`): it stubs `docker` on `PATH` and asserts the env gate fails
loudly on a missing/empty/whitespace var and on a missing `.env.production`, and
that a complete env builds and recreates the container. Run it with
`bash services/go-api/deploy/overseer_test.sh`.
