# Deploy runbook

Prod and staging share one VM (`$DEPLOY_HOST`). Commands run from `services/go-api` in
the VM's repo checkout. Script headers say what each script does; this page says when.

| Tier | URL | Env file on the VM | Containers |
|---|---|---|---|
| prod | `https://altune.duckdns.org` | `.env.production` | `altune-go-api-{blue,green}`, `altune-overseer`, `altune-redis`, `altune-caddy` |
| staging | `https://altune-staging.duckdns.org` | `.env.staging` | `altune-staging-go-api-{blue,green}`, `altune-staging-overseer`, `altune-staging-redis` |

Env files stay on the VM. Templates: [`../.env.example`](../.env.example), [`.env.staging.example`](.env.staging.example).

## Deploy flow

[`deploy-backend.yml`](../../../.github/workflows/deploy-backend.yml) runs on a push to
`main` under `services/go-api/**` or `services/overseer/**`, or by `workflow_dispatch`:

1. `test` + `test-overseer` on the commit being shipped.
2. `deploy-staging` runs [`staging.sh`](staging.sh): staging migrations, then recreates
   the `altune-staging-*` stack. Prod is untouched.
3. `smoke-staging` runs [`smoke.sh`](smoke.sh) against staging. Red blocks promotion.
4. `approve-prod` waits for a reviewer on the `production` environment.
5. `deploy-prod` runs [`prod-migrate.sh`](prod-migrate.sh), [`blue-green.sh`](blue-green.sh),
   [`overseer.sh`](overseer.sh), then `smoke.sh` against prod.

To approve: the pending *Deploy backend* run, Review deployments, `production`, Approve
and deploy. Reject leaves prod as it is.

If `deploy-prod` sits `pending` after approval and no other `deploy-prod` run is
`in_progress` or `queued`, GitHub's concurrency bookkeeping is wedged. Rename the job's
`concurrency.group` in `deploy-backend.yml` (for example `deploy-prod-v2` to
`deploy-prod-v3`) and push.

By hand (CI down, or a re-check):

```bash
git fetch origin && git checkout main && git reset --hard origin/main
bash deploy/staging.sh
bash deploy/smoke.sh https://altune-staging.duckdns.org altune-staging-overseer
bash deploy/prod-migrate.sh && bash deploy/blue-green.sh && bash deploy/overseer.sh
SMOKE_GOAPI_CONTAINER=altune-go-api-<live colour> bash deploy/smoke.sh https://altune.duckdns.org altune-overseer
```

## Prod migrations

Both tiers apply `migrations/*.sql` through the same runner in [`lib.sh`](lib.sh),
tracked in `schema_migrations`, each file once in `--single-transaction`. A failed
migration stops the deploy with the live colour still serving. There is no automatic
schema rollback; undo a migration by hand with `psql`.

A file that uses `CREATE|REINDEX|DROP INDEX CONCURRENTLY` runs without a transaction.
Declare it with a `-- migrate:no-transaction` header line. If its index build fails,
Postgres leaves an INVALID index that `IF NOT EXISTS` skips. Drop it before the retry:

```bash
U=$(grep -E '^DATABASE_URL=' .env.production | head -1 | sed -E 's/^DATABASE_URL=//')
psql "$U" -c "SELECT indexrelid::regclass FROM pg_index WHERE NOT indisvalid;"
psql "$U" -c "DROP INDEX CONCURRENTLY <invalid index>;"
```

`prod-migrate.sh` fails closed when the schema exists but `schema_migrations` is empty.
To seed the baseline once: apply any migration prod is missing by hand, in `sort -V`
order (`016` with `--single-transaction`; `020` and `021` without it), then record every
file as applied:

```bash
for v in $(for f in migrations/*.sql; do basename "$f" .sql; done | sort -V); do
  psql "$U" -v ON_ERROR_STOP=1 -c "INSERT INTO schema_migrations (version) VALUES ('$v') ON CONFLICT DO NOTHING;"
done
```

## Roll back

- Prod go-api: `bash deploy/rollback.sh` flips Caddy to the previous colour, health-gates
  it, and stops the bad one. `blue-green.sh` already reverts on a post-flip health failure.
- Overseer (either tier): redeploy the prior ref. It holds no schema.
- Staging: `bash deploy/staging.sh` or `docker compose -f deploy/compose.staging.yml up -d --force-recreate`.

## Staging tier

Staging is a separate Compose project (`staging`) with no host ports, reached through the
prod Caddy, and a separate Supabase project with its own users. Shape and reasons:
[design](../../../docs/features/staging-tier/design.md).

- The staging owner account mirrors the prod owner (same email and password); its UUID is
  `OVERSEER_OWNER_USER_ID`. On a fresh staging project, create the owner, then the
  read-only principal ([Overseer deploy](../../../docs/features/overseer/deploy.md#the-read-only-principal)).
- Keep these `.env.staging` keys staging-scoped so staging never writes to prod:
  `FEEDBACK_ENABLED=false` (or a scratch `GITHUB_ISSUE_REPO` and token),
  `BEHAVIORAL_CORPUS_PATH` empty, `OCI_S3_*` a key that is read-only on prod's bucket,
  `MUSICBRAINZ_USER_AGENT` a real staging contact. `compose.staging.yml` mounts the shared
  cookie jar and streamrip config read-only and forces `EVAL_METER_ENABLED=false`.

### Staging data from prod

[`staging-sync.yml`](../../../.github/workflows/staging-sync.yml) runs
[`staging-sync.sh`](staging-sync.sh) nightly (or on demand from Actions or the VM). Staging
reads prod's audio bucket with a read-only key: synced tracks play, new downloads fail.

### Web app

[`deploy-web.yml`](../../../.github/workflows/deploy-web.yml) exports `apps/mobile` for web
on a push under `apps/mobile/**`, releases it with [`web-release.sh`](web-release.sh) (its
`WEB_ROOT` is the host dir Caddy mounts read-only at `/srv/web`), and smoke-tests staging.
Prod follows after a `production` environment approval, with the same build, release and
smoke. Caddy serves a file from `<tier>/current` when the path matches one, otherwise
go-api. `<tier>` is `staging` or `prod`.

```bash
ls -t "$WEB_ROOT/<tier>/releases"                           # newest first
bash deploy/web-release.sh <tier> <previous-sha>            # roll back
bash deploy/web-release.sh <tier> <sha> /path/to/web.tgz    # release by hand
rm "$WEB_ROOT/<tier>/current"                               # web off; all paths go to go-api
```

A pruned sha (5 are kept) needs its tarball again: re-run the workflow for that commit.
If a build fails on an inline script hash, add the `sha256` it names to that tier's
`script-src` in the [`Caddyfile`](Caddyfile). New VM: `mkdir -p "$WEB_ROOT"` as the deploy
user before `docker compose -f deploy/compose.prod.yml up -d caddy`.

## Overseer ops

Env vars, the read-only principal, token self-healing and OCI cost access:
[Overseer deploy](../../../docs/features/overseer/deploy.md).

- Pause acquisition: set `ACQUISITION_PAUSED=true` in go-api's env and restart go-api.
- Disable jobs: `DISABLED_JOBS=eval meter,stream recovery`. An unknown name fails startup.
- Rotate the read-only password: change it in Supabase, update
  `OVERSEER_GOAPI_READONLY_PASSWORD`, then `bash deploy/overseer.sh`.

## CLIs on the VM

- `supabase`: logged in; use it for staging Supabase project ops.
- [`duckdns`](duckdns): DNS for `*.duckdns.org` (A and TXT records). `duckdns help` lists
  commands. It cannot create subdomains; do that on the duckdns.org site.

## Verify

`smoke.sh` is the check; its header lists what it asserts. Past it:

```bash
curl -s -o /dev/null -w '%{http_code}\n' https://altune.duckdns.org/overseer/api/buckets # 401
docker compose -f deploy/compose.prod.yml logs --since=25s overseer | grep -c collect.failed  # 0
```

Then sign in at `/overseer/` and confirm the buckets show live data, not `source_down`.
Only a real browser login proves owner auth. Script self-tests: `bash deploy/<script>_test.sh`.
