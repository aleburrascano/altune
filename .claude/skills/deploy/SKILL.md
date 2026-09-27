---
name: deploy
description: Operate altune's backend and web deploys on the shared VM - approve or unstick a prod promotion, release or roll back a commit by hand, clear a failed migration, and keep staging, overseer and the web app wired right. Use for a deploy that is red, pending or wrong, a rollback, a staging env change, overseer showing source_down or token errors, or a web release. Not for why a track failed to download (acquisition-debug) or for OCI resources outside the cost grant (oci).
---

# Deploy

Prod and staging share one VM, reached as `DEPLOY_USER@DEPLOY_HOST` (GitHub secrets). The checkout is `~/altune`; every command below runs from `~/altune/services/go-api` unless it says otherwise. Env files live only on the VM: `.env.production` and `.env.staging`, templated by `../.env.example` and `deploy/.env.staging.example`.

| Tier | URL | Containers |
|---|---|---|
| prod | `https://altune.duckdns.org` | `altune-go-api-{blue,green}`, `altune-overseer`, `altune-redis`, `altune-caddy` |
| staging | `https://altune-staging.duckdns.org` | `altune-staging-go-api-{blue,green}`, `altune-staging-overseer`, `altune-staging-redis` |

Any prod act (approving `production`, a prod `release.sh`, `rollback.sh`) waits for the operator's yes.

Read the sibling for its branch:
- [staging.md](staging.md) when creating or changing the staging tier, its env, or its data sync.
- [overseer.md](overseer.md) when overseer shows `source_down`, `STALE` cost, a token log line, or needs its principal set up.
- [web.md](web.md) when releasing, rolling back or turning off the web app.

## 1. Read where the release stands

`deploy-backend.yml` runs on a push to `main` touching backend code: tests, `deploy-staging`, `smoke-staging`, `approve-prod`, `deploy-prod`. Each deploy job runs `deploy/release.sh <tier> <sha>` from that commit.

```bash
gh run list --workflow deploy-backend.yml -L 5
gh run view <run-id>
```

Done when: you can name the failing or waiting job and the sha it carries.

## 2. Move it forward

- **Waiting on approval**: the operator approves the pending run's `production` deployment (Review deployments, Approve and deploy). Rejecting leaves prod as it is.
- **`deploy-prod` pending after approval** with no other `deploy-prod` run `in_progress` or `queued`: GitHub's concurrency bookkeeping is wedged. Cancel the stuck run, bump `concurrency.group` of `deploy-prod` in `deploy-backend.yml` (`deploy-prod-v2` to `deploy-prod-v3`) and merge it. A workflow-only change triggers no deploy, so start one with `gh workflow run deploy-backend.yml --ref main` and approve that run.
- **Red staging smoke**: prod is untouched; fix forward.
- **CI down, or a re-check**, on the VM:

  ```bash
  bash deploy/release.sh staging <sha>
  bash deploy/smoke.sh https://altune-staging.duckdns.org altune-staging-overseer <sha>
  bash deploy/release.sh prod <sha> https://altune.duckdns.org
  ```

Done when: `curl -s https://<tier-host>/health` reports the sha you meant to ship.

## 3. Roll back

- Prod go-api: `bash deploy/rollback.sh` flips Caddy to the previous colour, health-gates it and stops the bad one. `blue-green.sh` already reverts on a post-flip health failure.
- Overseer, either tier: release the prior sha. It holds no schema.
- Staging: `bash deploy/release.sh staging <prior-sha>`.
- A migration: no automatic schema rollback. Undo it by hand with `psql`.

Done when: `/health` reports the prior sha and `bash deploy/smoke.sh` passes for that tier.

## 4. Clear a failed migration

Both tiers apply `migrations/*.sql` once each, tracked in `schema_migrations`. A failure stops the release with the live colour still serving. A `CONCURRENTLY` index build that fails leaves an INVALID index that `IF NOT EXISTS` skips on the retry. Drop it first:

```bash
U=$(grep -E '^DATABASE_URL=' .env.production | head -1 | sed -E 's/^DATABASE_URL=//')
psql "$U" -c "SELECT indexrelid::regclass FROM pg_index WHERE NOT indisvalid;"
psql "$U" -c "DROP INDEX CONCURRENTLY <invalid index>;"
```

Done when: the query returns no rows and the release is re-run green.

## Switches

Set in the tier's env file, then release the running sha again so go-api picks it up.

- `ACQUISITION_PAUSED=true` pauses acquisition.
- `DISABLED_JOBS=eval meter,stream recovery` turns jobs off. An unknown name fails startup.

## VM tools

- `supabase`: logged in, for staging Supabase project ops.
- `deploy/duckdns`: A and TXT records for `*.duckdns.org`; `duckdns help` lists commands. New subdomains are made on the duckdns.org site.
- Script self-tests: `bash deploy/<script>_test.sh`.
