# Deploy runbook

Operational deploy steps for the OCI prod VM (`altune.duckdns.org`, repo at
`/home/ubuntu/altune`). This lives next to the deploy machinery on purpose: the
release chair (`ship`) reads it here; no other context needs it.

Merge to `main` now ships through a **two-tier pipeline** — staging first, then a
manually approved prod promotion. `.github/workflows/deploy-backend.yml` runs it on
any push to `main` under `services/go-api/**` or `services/overseer/**` (also
`workflow_dispatch`). Prod is touched **only** after a green staging smoke and a
human approval.

## The two-tier pipeline

The jobs run in order; each gates the next (`deploy-backend.yml`):

1. **`test` + `test-overseer`** — the two suites, run against the exact commit being
   shipped. A red suite stops everything.
2. **`deploy-staging`** — SSHes to the VM, fast-forwards `main`
   (`git reset --hard origin/main`), and runs `deploy/staging.sh`. That
   **applies staging migrations** and rebuilds/recreates **only** the
   `altune-staging-*` stack. Prod containers are never touched here.
3. **`smoke-staging`** — runs `deploy/smoke.sh https://altune-staging.duckdns.org
   altune-staging-overseer`. A red gate blocks promotion.
4. **`deploy-prod`** — gated on the GitHub **`production` environment** (manual
   approval by the required reviewer). On approval it applies any new prod migrations
   (`prod-migrate.sh`), then runs the prod deploy (`blue-green.sh` + `overseer.sh`).
   This is the **only** job that touches prod.

## Deploy to staging (automatic)

`deploy-staging` runs `deploy/staging.sh` on every qualifying merge — nothing manual.
The script (`COMPOSE_FILE=deploy/compose.staging.yml`, project `staging`):

- **requires** `.env.staging` on the VM with non-empty `DATABASE_URL`,
  `OVERSEER_SUPABASE_URL`, `OVERSEER_SUPABASE_ANON_KEY`, `OVERSEER_OWNER_USER_ID`;
  a missing var fails the deploy before anything is built.
- **auto-applies migrations** to the staging Supabase DB. It tracks applied versions
  in a `schema_migrations` table (one row per `migrations/*.sql`, ordered `sort -V`)
  and applies each unapplied file once inside `--single-transaction` (except
  no-transaction files — see below). A DB already migrated by hand (table empty but
  `public.tracks` exists) is **adopted** at the current baseline so a non-idempotent
  migration is never re-run.
- rebuilds and recreates `go-api-blue`, `overseer`, and `redis` in place — no
  blue-green flip on staging (a brief staging blip is accepted; Caddy's
  `staging-upstream.conf` already points at `altune-staging-go-api-blue`).
- waits up to 180s for `https://altune-staging.duckdns.org/health`; if it never turns
  healthy the deploy fails (dumping `go-api-blue` logs), which blocks promotion.

To re-run staging by hand: re-trigger the workflow (`workflow_dispatch`), or on the
VM `cd /home/ubuntu/altune/services/go-api && bash deploy/staging.sh`.

## The smoke gate

`smoke-staging` runs `deploy/smoke.sh <base-url> <overseer-container>` on the VM. It
fails (blocking prod promotion) unless **all** hold:

- `https://altune-staging.duckdns.org/health` → **200**
- `https://altune-staging.duckdns.org/overseer/` → **200** (SPA served)
- no operator-token persistence/seed failure in the last 30s of the
  `altune-staging-overseer` logs (the #1471 class: `permission denied`,
  `persisting rotated refresh token failed`, `refresh_token_already_used`,
  `read-only token refresh failed at status: status 400`,
  `read-only token refresh failed at password_grant`).

A generic `overseer.collect.failed` (e.g. the OCI-usage 404, #1487) is **tolerated** —
only token/persist breakage fails the gate. This gate runs on **staging only** —
`deploy-prod` does **not** run `smoke.sh`; prod is gated by `blue-green.sh`'s
health check plus `overseer.sh`'s own token-failure self-verify. The script is
tier-agnostic, so you *can* run it against prod **by hand** after a promotion for
an extra cross-check: `bash deploy/smoke.sh https://altune.duckdns.org altune-overseer`.

## Promote to prod (approve / deny)

After a green smoke, `deploy-prod` waits on the `production` environment's required
reviewer. To act on it:

1. **GitHub → Actions →** the pending *Deploy backend* run.
2. Click **Review deployments**.
3. Tick **`production`**, then **Approve and deploy** (optionally with a comment) to
   promote, or **Reject** to deny. Rejecting leaves prod on its current version;
   nothing was touched.

**`deploy-prod` stuck `pending` after approval:** the job sits `pending` forever
even though it was approved and no other `deploy-prod` run is in progress. This
is GitHub's own concurrency-group bookkeeping wedging on the group name, not a
real lock held by another run. Check: **Actions →** confirm no other `deploy-prod`
job is actually `in_progress` or `queued`. Fix: rename the job's `concurrency.group`
in `.github/workflows/deploy-backend.yml` (e.g. `deploy-prod` → `deploy-prod-v2`)
and push; the new group name clears the stale lock. Renaming again is the fix if
it recurs.

On approval, `deploy-prod` SSHes in, fast-forwards `main`, applies new prod
migrations (`prod-migrate.sh`), then runs `blue-green.sh` (builds the idle colour,
health-gates it, flips Caddy) then `overseer.sh` — all detailed below.

## Prod migrations (automatic once baselined, #1525)

`deploy-prod` runs `deploy/prod-migrate.sh` on the VM **before** the blue-green swap,
inside the human-approved job. It shares lib.sh's migration runner with `staging.sh`
so the two can never diverge. Specifically it:

- greps `DATABASE_URL` from `.env.production` (never `source`s it — `.env` values can
  hold unquoted parens; the URL is never logged). A missing file or empty
  `DATABASE_URL` fails the deploy before anything is applied.
- tracks applied versions in `schema_migrations` and applies each unapplied
  `migrations/*.sql` once inside `--single-transaction` (a half-applied migration
  rolls back).
- **fails fast**: a migration error aborts the deploy (`set -euo pipefail`) with the
  live colour still serving — nothing swaps onto a schema that never migrated.

Concurrent deploys can't race: the workflow's `deploy-backend` concurrency group
serializes runs.

### No-transaction migrations (#1550)

`CREATE INDEX CONCURRENTLY` (and `REINDEX`/`DROP INDEX CONCURRENTLY`) are rejected by
Postgres inside a transaction block, so the runner drops `--single-transaction` for a
file that declares itself no-transaction. Declare one with a header line:

```sql
-- migrate:no-transaction
```

A file that uses `CONCURRENTLY` in a statement but forgot the header is detected
anyway (comments are stripped first, so merely *mentioning* it in prose keeps the
file's transaction). Both tiers use the same detection — staging proves it first.

Such a file has no rollback: if the index build fails, the tracker INSERT is never
reached, so the version stays unapplied and the next deploy retries it — but Postgres
leaves an **INVALID** index behind that `IF NOT EXISTS` would then skip. Drop it
before the retry:

```bash
psql "$U" -c "SELECT indexrelid::regclass FROM pg_index WHERE NOT indisvalid;"
psql "$U" -c "DROP INDEX CONCURRENTLY <the invalid index>;"
```

### One-time baseline (REQUIRED before the first automated run)

Unlike staging, prod-migrate.sh does **not** blind-adopt the schema as the baseline.
A #1525 dry-run against the live prod DB found prod is **not** in lockstep — it has
**001–015 and 017** applied but is **missing 016, 018, 019, 020, 021, 022** (a
forgotten-migration backlog from the old manual process). Auto-adopting would mark
those "applied" and skip them forever, including the `016` UNIQUE data-integrity
constraint. So when `schema_migrations` is empty but the schema exists, prod-migrate.sh
**fails closed** with a message pointing here rather than guessing.

To clear it (once), an operator with knowledge of prod's real state must:

1. **Apply the genuinely-missing migrations by hand**, in `sort -V` order. Most are
   idempotent (`IF NOT EXISTS`); two are not run-of-the-mill:
   - `016` is non-idempotent (bare `ADD CONSTRAINT`) and has a heal step — apply once
     with `psql "$U" -v ON_ERROR_STOP=1 --single-transaction -f migrations/016_*.sql`.
   - `020` and `021` use `CREATE INDEX CONCURRENTLY`, which **cannot** run inside a
     transaction — apply them with plain `psql "$U" -v ON_ERROR_STOP=1 -f …` (no
     `--single-transaction`). Both carry the `-- migrate:no-transaction` header, so
     the runner would handle them the same way (see *No-transaction migrations*); by
     hand here, just don't wrap them.
2. **Seed the tracker** so it reflects the now-complete set:
   ```bash
   cd /home/ubuntu/altune/services/go-api
   U=$(grep -E '^DATABASE_URL=' .env.production | head -1 | sed -E 's/^DATABASE_URL=//')
   for v in $(for f in migrations/*.sql; do basename "$f" .sql; done | sort -V); do
     psql "$U" -v ON_ERROR_STOP=1 \
       -c "INSERT INTO schema_migrations (version) VALUES ('$v') ON CONFLICT DO NOTHING;"
   done
   ```

From then on, every future migration auto-applies on deploy with no manual step. To
run it by hand (e.g. re-checking after a fix):
`cd /home/ubuntu/altune/services/go-api && bash deploy/prod-migrate.sh`.

## Roll back

**Prod** (the command the workflow's failure step also prints):

```bash
cd /home/ubuntu/altune/services/go-api && bash deploy/rollback.sh
```

`rollback.sh` flips Caddy back to the previously-active go-api colour, health-gates
it, and stops the bad colour. overseer is in-memory (no migration) — roll it back by
redeploying the prior ref. A prod **schema** rollback, if a migration must be undone,
is also by hand (`psql` against the prod DB); there is no auto-rollback of migrations
on either tier.

**Staging**: no promotion depends on it, so just redeploy or recreate the stack:

```bash
cd /home/ubuntu/altune/services/go-api
bash deploy/staging.sh                                               # redeploy current main
docker compose -f deploy/compose.staging.yml up -d --force-recreate  # recreate altune-staging-*
```

## Web app (staging)

`.github/workflows/deploy-web.yml` ships the Expo web export of `apps/mobile` on every
push to `main` under `apps/mobile/**` (also `workflow_dispatch`):

1. **`test`** — the mobile suite, against the exact commit being shipped.
2. **`build-staging`** — `npx expo export -p web` with the staging API and Supabase
   env, fails if any inline `<script>` in the export is missing from the staging CSP's
   `script-src` hashes, then packs `dist/` as `web.tgz` beside `deploy/web-release.sh`.
3. **`deploy-staging`** — copies both to `/home/ubuntu/altune-web-incoming/<sha>/` and
   runs `web-release.sh staging <sha> web.tgz` there. It never touches the repo
   checkout or any container.
4. **`smoke-staging`** — `GET /` is 200 HTML carrying the CSP and the entry bundle just
   built; `/health` is go-api's JSON; `/overseer/` is 200.

**Layout on the VM.** `/home/ubuntu/altune-web/<tier>/releases/<sha>/` holds an unpacked
export; `/home/ubuntu/altune-web/<tier>/current` is a **relative** link to
`releases/<sha>`, so it resolves inside the Caddy container, which mounts
`/home/ubuntu/altune-web` read-only at `/srv/web`. `web-release.sh` unpacks into a temp
dir, refuses an export without a root `index.html`, renames the new link over `current`
(`mv -T`, atomic), and keeps the newest 5 releases. A failed unpack exits non-zero with
`current` untouched. Caddy reads through the link per request: **no reload** for a web
release.

**Routing.** The staging site block serves a file only when the path maps to one under
`/srv/web/staging/current` (`{path}`, `{path}.html`, `{path}/index.html`); everything
else falls through to go-api exactly as before, and the CSP / `nosniff` /
`Referrer-Policy` headers apply to web responses only. Caddy never lists API paths.
`deploy/caddy-routing_test.sh` proves this against the real Caddy image.

**One-time bring-up (a prod act, needs the operator's yes).** The mount lives on the
shared `altune-caddy` in `compose.prod.yml`, so enabling it recreates the Caddy that
serves prod and staging (a few seconds of blip on both):

```bash
mkdir -p /home/ubuntu/altune-web     # as ubuntu, BEFORE the recreate, or Docker creates it root-owned
cd /home/ubuntu/altune/services/go-api
docker compose -f deploy/compose.prod.yml up -d caddy
```

Until then `deploy-web` releases land on disk but `smoke-staging` stays red.

**Release or roll back by hand.**

```bash
ls -t /home/ubuntu/altune-web/staging/releases                   # newest first; current is one of them
bash deploy/web-release.sh staging <previous-sha>                # roll back: re-points current, no tarball needed
bash deploy/web-release.sh staging <sha> /path/to/web.tgz        # release a tarball by hand
```

A sha that has been pruned needs its tarball again (re-run the workflow for that commit).

**Turn the web app off without a deploy:** `rm /home/ubuntu/altune-web/staging/current`.
Every path then falls through to go-api, exactly as before the web tier existed.

**CSP and inline scripts.** Expo's static export inlines one hydration script. Its
`sha256` is in the staging `script-src`; `'unsafe-inline'` never is. If an Expo upgrade
changes that script, `build-staging` fails naming the new hash to add in the Caddyfile.

## Staging tier facts

- **Entrypoint:** `https://altune-staging.duckdns.org` — a separate origin (own
  cookies/CORS/JWT audience), served by the shared prod Caddy. go-api is a **pure
  API**; the root path serves the web app once a web release is live (see "Web app"
  below), and **404s** from go-api when none is. Use `/health` and `/overseer/`.
- **Supabase:** a **separate** project, ref **`ijyjoyxhwmbmriwzazbx`** (prod is
  `ellvexundmgvbbfqbzau`). Own auth realm, own `auth.users`, full data isolation.
- **Secrets:** `services/go-api/.env.staging` on the VM only — **never committed.**
- **Containers:** `altune-staging-go-api-blue` / `-green`, `altune-staging-overseer`,
  `altune-staging-redis` (project `staging`, no host-port binds, `mem_limit`/`cpus`
  caps; reached only through the prod Caddy over the shared `go-api_default` network).

### Staging dashboard access

The staging Supabase owner **mirrors the prod owner**: email
`aleburrascano123@gmail.com`, same password. That account's UUID is
`OVERSEER_OWNER_USER_ID` / `OPERATOR_USER_ID` in `.env.staging`. Sign in at
`https://altune-staging.duckdns.org/overseer/` to view the staging dashboard.
Overseer's own go-api credential is a **separate read-only account**
(`OPERATOR_READONLY_USER_ID` + `OVERSEER_GOAPI_READONLY_EMAIL` /
`OVERSEER_GOAPI_READONLY_PASSWORD`, signed in and then persisted/rotated like prod's).

**A fresh staging Supabase project needs this bootstrap repeated:** create the
owner account, put its UUID in the two `.env.staging` ids, then create the
read-only account and do the #1810 bootstrap below for it.

### CLIs on the VM for staging / DNS ops

- **`supabase`** — authenticated; use it for staging Supabase project ops.
- **`duckdns`** — wrapper with the token saved; use it for `*.duckdns.org` DNS ops
  (e.g. the `altune-staging` A record).

## Prod deploy — go-api (blue-green)

`deploy-prod` runs `deploy/blue-green.sh` (builds + flips
`go-api-blue`/`go-api-green`). A build or pre-flip health failure leaves the live
colour serving and exits non-zero; a post-flip health failure rolls the upstream back
automatically.

## Prod deploy — overseer

`deploy-prod` runs `deploy/overseer.sh` right after `blue-green.sh`. That script
builds the image and `up -d overseer` (a single in-memory container, no blue-green
swap — a brief `/overseer` blip, go-api traffic untouched), then:

- **chowns the data volume** to uid 1000 if a pre-fix volume is still `root:root`,
  and force-recreates, so overseer can write its token file (`#1471`);
- **self-verifies**: waits one collect cycle, then fails the deploy if overseer is
  not `healthy` or its logs show operator-token persistence/seed breakage (a
  generic `collect.failed`, e.g. the OCI-usage 404 `#1487`, does **not** fail it).

The env check runs before the container is touched, so a missing required
`OVERSEER_*` var fails the deploy loudly instead of crash-looping in prod. The
script itself needs no manual step (the human gate is upstream, on `deploy-prod`).
Full detail: `docs/features/overseer/deploy.md`.

Caddy already routes `/overseer/*` → `altune-overseer:8090` (`deploy/Caddyfile`),
unchanged. overseer is in-memory only — no DB migration, so rollback is just
redeploying the prior ref.

Overseer reads go-api through Caddy's internal-only listeners (#2361):
`altune-caddy:8081` imports `upstream.conf` (prod), `altune-caddy:8082` imports
`staging-upstream.conf` (staging). They follow every blue/green flip and rollback
with no overseer restart, and are never host-published. **Operator step, once per
tier:** confirm the listener answers
(`docker exec altune-overseer wget -q -O - http://altune-caddy:8081/health`;
recreate Caddy if refused), set `OVERSEER_GOAPI_URL=http://altune-caddy:8081` in
`.env.production` (`:8082` in `.env.staging`), then recreate overseer. Full steps
and rollback: *Reading go-api through Caddy* in `docs/features/overseer/deploy.md`.

### Required env (`services/go-api/.env.production`)

The new binary **fails closed / crash-loops** without these:

- `OVERSEER_OWNER_USER_ID` — the owner's Supabase user id (UUID). The allowlist.
- `OVERSEER_SUPABASE_URL`, `OVERSEER_SUPABASE_ANON_KEY` — public; also served to
  the SPA at `/config.json` so it can init supabase-js for login.
- `OVERSEER_GOAPI_URL` — go-api base the buckets read: `http://altune-caddy:8081`,
  the internal Caddy listener (see above).
- `OVERSEER_GOAPI_READONLY_EMAIL`, `OVERSEER_GOAPI_READONLY_PASSWORD` — the
  **read-only** principal's Supabase sign-in (NOT the operator's; see *The read-only
  principal* below). When the refresh token is rejected (`400`) and the persisted
  file holds nothing newer, overseer signs this account in again with the Supabase
  password grant and persists the new refresh token, so the credential heals with no
  human step. Both must be set; without them a `400` backs off as before.
- `OVERSEER_GOAPI_READONLY_REFRESH_TOKEN` — optional seed for the read-only chain.
  Overseer rotates it and persists the live one to the `overseer-data` volume
  (`/var/lib/overseer/readonly_refresh_token`, chmod 600), so restarts resume the
  chain. With the email/password pair set you can leave it unset: the first boot
  signs in. A leftover `OVERSEER_GOAPI_REFRESH_TOKEN` /
  `OVERSEER_GOAPI_TOKEN` is **ignored** (overseer logs `ignored_var=…`), never used
  as a fallback.
- `OVERSEER_BASE_PATH=/overseer`, `OVERSEER_OCI_ENABLED` (cost bucket). Enabling
  it also needs a one-time OCI IAM grant on the instance principal — see
  "OCI cost access" in `docs/features/overseer/deploy.md`.
- **Not** `OVERSEER_OWNER_TOKEN` — retired with the old cookie dashboard.

### The read-only principal (#1810)

Overseer authenticates to `/admin/*` as a **second Supabase user that is not the
operator**. go-api admits that subject on admin GETs only and answers **403
`admin.read_only_forbidden`** on every mutating admin route, so the credential
overseer holds — and persists to disk — cannot change production if it leaks.

Bootstrap (once per Supabase project, before the deploy that needs it):

1. Create a Supabase user for overseer (e.g. `overseer-readonly@altune.app`) in
   the project's auth realm. It must **not** be the owner/operator account.
2. Put its UUID in **`OPERATOR_READONLY_USER_ID`** (go-api's env — `.env.production`).
   go-api refuses to start if it is not a UUID, or if it equals `OPERATOR_USER_ID`.
3. Put its email and password in **`OVERSEER_GOAPI_READONLY_EMAIL`** and
   **`OVERSEER_GOAPI_READONLY_PASSWORD`** (`.env.production`). Overseer signs in with
   them on first boot and whenever its refresh chain dies.

Skipping this leaves the admin surface operator-only: overseer's reads get 403 and
every go-api-backed bucket shows `source_down`. That is the deliberate fail-closed
direction — overseer never falls back to the operator credential.

### Pause acquisition / disable a job

Overseer is observe-only: there is no runtime POST to pause acquisition or flip a
background job anymore. Both are startup settings on go-api instead — set the env
var on the server and restart:

- `ACQUISITION_PAUSED=true` pauses the background acquisition scheduler.
- `DISABLED_JOBS=eval meter,stream recovery` (comma-separated job names) starts
  each named job disabled. An unknown name fails go-api's startup.

### Refresh-token rotation (self-healing — no manual reseed)

Supabase rotates the read-only refresh token on every use. Overseer persists the
rotated token to the `overseer-data` volume
(`/var/lib/overseer/readonly_refresh_token`), so a restart resumes the live chain
instead of replaying the spent seed. Just `up -d overseer`.

If the chain dies anyway (something else spent the token, the volume was wiped),
the refresh answers `400`, overseer re-reads the file once, and when the file holds
nothing newer it signs the read-only account in again with
`OVERSEER_GOAPI_READONLY_EMAIL` / `OVERSEER_GOAPI_READONLY_PASSWORD` and persists
the new refresh token. It logs `read-only account signed in again with the password
grant` (never the password or a token). There is no incognito reseed.

A failed sign-in (`400` bad credentials, `429` rate limit) backs off on the same
capped curve as a failed refresh, at most one attempt per window, and logs
`read-only token refresh failed at password_grant`. That line fails the deploy
self-verify and the smoke gate. Check the pair in `.env.production`.

**Rotating the read-only password:** change it in Supabase, update
`OVERSEER_GOAPI_READONLY_PASSWORD` in `.env.production`, then `up -d overseer`. The
live refresh chain keeps working across the change; the new password is only used
the next time the chain dies.

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
