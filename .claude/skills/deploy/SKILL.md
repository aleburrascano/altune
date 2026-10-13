---
name: deploy
description: Operate altune's backend and web deploys on the shared VM - unstick a deploy, check that prod follows a green staging, release or roll back a commit by hand, clear a failed migration, and keep staging, overseer and the web app wired right. Use for a deploy that is red, pending or wrong, a rollback, a staging env change, overseer showing source_down or token errors, or a web release. Not for why a track failed to download (acquisition-debug) or for OCI resources outside the cost grant (oci).
---

# Deploy

Prod and staging share one VM. The deploy workflows run on the self-hosted `host` runner and ssh in as `ubuntu@altune.duckdns.org` with a key file on the runner, so they need no SSH or `DEPLOY_*` Actions secrets; by hand, reach it with the same login. The checkout is `~/altune`; every command below runs from `~/altune/services/go-api` unless it says otherwise. Env files live only on the VM: `.env.production` and `.env.staging`, templated by `../.env.example` and `deploy/.env.staging.example`.

| Tier | URL | Containers |
|---|---|---|
| prod | `https://altune.duckdns.org` | `altune-go-api-{blue,green}`, `altune-overseer`, `altune-redis`, `altune-caddy` |
| staging | `https://altune-staging.duckdns.org` | `altune-staging-go-api-{blue,green}`, `altune-staging-overseer`, `altune-staging-redis` |

Prod acts (a prod `release.sh`, `rollback.sh`) run without a human yes (operator decision, 2026-09-27): release to prod only after the same sha is green on staging, and roll back on a failed prod smoke.

Read the sibling for its branch:
- [staging.md](staging.md) when creating or changing the staging tier, its env, or its one-way data copy from prod, and the nightly auth drift check.
- [overseer.md](overseer.md) when overseer shows `source_down`, `STALE` cost, a token log line, or needs its principal set up.
- [web.md](web.md) when releasing, rolling back or turning off the web app.

## 1. Read where the release stands

The repo is on the self-hosted Gitea now (remote `gitea`, CLI `forge`, the gh-shaped wrapper). Gitea Actions reads `.gitea/workflows/` and ignores `.github/workflows/` once that folder exists, and it holds `precheck.yml`, `deploy.yml`, `deploy-web.yml` and `staging-sync.yml`, among others. `.github/workflows/` holds only the iOS release, which needs GitHub's macOS runner. GitHub is otherwise a read mirror of `main`, pushed by `github-mirror.yml`; Gitea holds the source and the issues. Every script a workflow runs lives in `scripts/ci/`. `deploy.yml` releases staging, smokes it, then releases prod. It runs only when started by hand (`workflow_dispatch`), so a merge to `main` never releases. The by-hand path in section 2, `deploy/release.sh <tier> <sha>` on the VM, stays for reruns and rollbacks.

Where `main` stands, and what the VM runs:

```bash
git fetch gitea main && git log -1 --format='%h %s' gitea/main
forge run list --branch main -L 5          # precheck and deploy runs
ssh ubuntu@altune.duckdns.org 'cd ~/altune && git log -1 --format=%h'
```

Done when: you can name the sha on `main` and the sha each tier runs.

## 2. Move it forward

- **The staging proof**, with nothing after it, is the only gate: `deploy.yml` and `deploy-web.yml` go on to prod only after the staging smoke passes, and by hand run the prod line below only once staging passed its smoke for the same sha.
- **Red staging smoke**: prod is untouched; fix forward.
- **A rerun or a re-check**, by hand on the VM:

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
- A migration: no automatic schema rollback. Restore the pre-migration snapshot with `bash deploy/restore-snapshot.sh <staging|prod> <dump file>`, or undo it by hand with `psql`.

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
- Script self-tests: `bash deploy/__tests__/<script>.test.sh`.
