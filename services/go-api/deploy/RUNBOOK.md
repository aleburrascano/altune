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
  `operator token refresh failed at status: status 400`).

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

## Staging tier facts

- **Entrypoint:** `https://altune-staging.duckdns.org` — a separate origin (own
  cookies/CORS/JWT audience), served by the shared prod Caddy. go-api is a **pure
  API**: the root path **404s**. Use `/health` and `/overseer/`.
- **Supabase:** a **separate** project, ref **`ijyjoyxhwmbmriwzazbx`** (prod is
  `ellvexundmgvbbfqbzau`). Own auth realm, own `auth.users`, full data isolation.
- **Secrets:** `services/go-api/.env.staging` on the VM only — **never committed.**
- **Containers:** `altune-staging-go-api-blue` / `-green`, `altune-staging-overseer`,
  `altune-staging-redis` (project `staging`, no host-port binds, `mem_limit`/`cpus`
  caps; reached only through the prod Caddy over the shared `go-api_default` network).

### Staging dashboard access

The staging Supabase owner **mirrors the prod owner**: email
`aleburrascano123@gmail.com`, same password. That account's UUID is
`OVERSEER_OWNER_USER_ID` / `OPERATOR_USER_ID` in `.env.staging`, and its operator
refresh token is seeded there (then persisted/rotated like prod's). Sign in at
`https://altune-staging.duckdns.org/overseer/` to view the staging dashboard.

**A fresh staging Supabase project needs this owner bootstrap repeated:** create the
owner account, put its UUID in the two `.env.staging` ids, and seed a fresh
operator refresh token.

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
