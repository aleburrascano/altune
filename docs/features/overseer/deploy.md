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

- `OVERSEER_GOAPI_URL` — go-api base the buckets read. Prod: `http://altune-caddy:8081`,
  staging: `http://altune-caddy:8082` (see *Reading go-api through Caddy* below).
- `OVERSEER_GOAPI_READONLY_EMAIL`, `OVERSEER_GOAPI_READONLY_PASSWORD` — the
  **read-only** principal's Supabase sign-in (#1810): a service account that is NOT
  the operator, which go-api admits on admin GETs and refuses (403) on every
  mutating admin route, so a leaked password can read, not write. Its UUID goes in
  go-api's `OPERATOR_READONLY_USER_ID`. Both must be set for self-healing.
- `OVERSEER_GOAPI_READONLY_REFRESH_TOKEN` — optional seed for the read-only refresh
  chain; unnecessary once the email/password pair is set. A leftover
  `OVERSEER_GOAPI_REFRESH_TOKEN`/`OVERSEER_GOAPI_TOKEN` is ignored, never used as a
  fallback — with no read-only credential the buckets go `source_down`.
  **See the gotcha below.**
- `OVERSEER_BASE_PATH=/overseer`, `OVERSEER_OCI_ENABLED` (cost bucket).
- **Not** `OVERSEER_OWNER_TOKEN` — retired with the old cookie dashboard.

### GOTCHA: the read-only refresh token is single-use and rotates

Supabase rotates refresh tokens on every use, and a spent token answers
`status 400` (every go-api bucket shows `source_down`, dashboard still serves).
Overseer persists each rotated token to the `overseer-data` volume at
`/var/lib/overseer/readonly_refresh_token` (chmod 600), so a restart resumes the
live chain instead of replaying the spent seed in `.env.production`. The file wins
over the env seed whenever it holds a token.

The file is guarded by a sibling lock, `readonly_refresh_token.lock`. Every
rotation holds that lock from reading the file, through the exchange, to writing
the rotated token back, so two overseer processes on one volume never spend the
same token. When the file changed since overseer last read or wrote it, overseer
adopts the file's token before exchanging. After a `400` it re-reads the file once
and, if the file holds a different token, retries with it immediately instead of
backing off. A failed write is logged (`persisting rotated refresh token failed`,
never the token) and overseer keeps the rotated token in memory, so the chain
lives until the next restart; fix the volume before then. If the token directory
cannot be created or locked at boot, or the file exists but cannot be read
(permissions, EIO), overseer logs `refresh token file unusable, continuing
unpersisted` and starts from the env seed instead of the file (signing in with
the password grant below if that seed is spent or unset); after boot the same log
line means rotations are held only in memory.

If the chain is truly lost (wiped volume, or `status 400` with no newer token on
disk), overseer signs the read-only account in again with the Supabase password
grant (`POST {OVERSEER_SUPABASE_URL}/auth/v1/token?grant_type=password`, the anon
key as `apikey`) using `OVERSEER_GOAPI_READONLY_EMAIL` /
`OVERSEER_GOAPI_READONLY_PASSWORD`, persists the new refresh token through the same
locked file, and logs `read-only account signed in again with the password grant`.
No human step, no incognito reseed.

Supabase rate-limits the password grant, so a failed sign-in (`400`, `429`) backs
off on the same capped curve as a failed refresh: at most one attempt per window.
It logs `read-only token refresh failed at password_grant` (never the password or
a token), which the deploy self-verify and smoke gate treat as a token failure.
Without the two vars a `400` backs off as before and the buckets stay `source_down`.

To rotate the read-only password: change it in Supabase, update
`OVERSEER_GOAPI_READONLY_PASSWORD` on the VM, then let the deploy run (or `up -d
overseer`). The live chain is unaffected; the new password is used the next time
the chain dies.

## Reading go-api through Caddy (#2361)

Overseer reads go-api over the Docker network, not out through DuckDNS and back
in. `services/go-api/deploy/Caddyfile` has two internal-only plain-HTTP sites:

| Listener | Imports | Serves |
|---|---|---|
| `altune-caddy:8081` | `/etc/caddy/upstream.conf` | prod go-api, the active blue/green colour |
| `altune-caddy:8082` | `/etc/caddy/staging-upstream.conf` | staging go-api, the active staging colour |

Each imports the same upstream file as its public site, so `flip_to` and
`restore_upstream` (`deploy/lib.sh`) move overseer with the public traffic on the
same `caddy reload`, with no overseer restart. Neither port is published to the
host: `compose.prod.yml` lists them under `expose` (documentation only) and maps
only 80/443. `blue-green_test.sh` asserts the listener follows a flip and a
rollback and that neither port is ever published.

### Operator step (human only, once per tier)

The repo cannot change `.env.production` / `.env.staging` on the VM. After the
deploy that ships this change:

1. Confirm Caddy has the listener. The Caddyfile is a single-file bind mount, so
   a running Caddy only sees the new file once it is recreated; the merge deploy
   does that because the `caddy` service's compose config changed. Check:

   ```bash
   cd /home/ubuntu/altune/services/go-api
   docker exec altune-overseer wget -q -O - http://altune-caddy:8081/health          # go-api health JSON
   docker exec altune-staging-overseer wget -q -O - http://altune-caddy:8082/health  # staging go-api health
   ```

   If either fails with connection refused, recreate Caddy
   (`docker compose -f deploy/compose.prod.yml up -d --force-recreate caddy`, a
   seconds-long 80/443 blip) and check again.
2. In `services/go-api/.env.production` set
   `OVERSEER_GOAPI_URL=http://altune-caddy:8081`; in `services/go-api/.env.staging`
   set `OVERSEER_GOAPI_URL=http://altune-caddy:8082`.
3. Recreate overseer so it reads the new env: `bash deploy/overseer.sh` for prod,
   `docker compose -f deploy/compose.staging.yml up -d --force-recreate overseer`
   for staging.
4. Sign in at `/overseer/` and confirm the go-api buckets show live data.

Rollback: set `OVERSEER_GOAPI_URL` back to the public URL
(`https://altune.duckdns.org` / `https://altune-staging.duckdns.org`) and recreate
overseer.

## OCI cost access

The cost bucket's spend half (`services/overseer/internal/buckets/cost/cost.go`
`spendReader`) reads OCI's usage-api through the instance principal — no stored
key, so there is nothing to rotate, but the tenancy has to grant that principal
the read explicitly. Until it does, prod logs `cost: oci spend ... usage-api
denied access (HTTP 404 NotAuthorizedOrNotFound): the instance principal lacks
usage-api read` on every spend refresh (hourly) and the spend half renders
`STALE` forever — no crash, just an empty half of the panel. This is a one-time
operator step; a human applies it, do not attempt it from a container or CI.

1. In the OCI console, **Identity & Security → Domains → Dynamic Groups**, create
   (or confirm) a dynamic group matching the prod instance, e.g. matching rule
   `instance.compartment.id = '<compartment-ocid>'`. Name it (the code's hint
   string and `docs/features/cost/notes.md` call it `overseer-instances`).
2. In **Identity & Security → Policies**, add a policy in the tenancy's root
   compartment with this exact statement — the verb is `read`, the resource
   type is OCI's fixed public grant target `usage-report` (not a compartment
   resource, so it is always scoped `in tenancy`, never a compartment):

   ```
   Allow dynamic-group overseer-instances to read usage-report in tenancy
   ```

   `usage-report` carries no tenancy identifier of its own — it is OCI's name
   for the billing/usage aggregation, the same resource type `oci usage-api
   request-summarized-usages` reads under the hood.
3. Confirm: after the next hourly spend refresh (or restart overseer to force
   one), sign in at `/overseer/` and check the Cost panel — the spend half
   should show a live figure, not `STALE`, and
   `docker compose -f deploy/compose.prod.yml logs overseer | grep "usage-api denied"`
   should return nothing new.

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
