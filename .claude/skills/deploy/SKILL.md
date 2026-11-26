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
U=$(grep -E '^MIGRATE_DATABASE_URL=' .env.migrate.production | head -1 | sed -E 's/^MIGRATE_DATABASE_URL=//')
psql "$U" -c "SELECT indexrelid::regclass FROM pg_index WHERE NOT indisvalid;"
psql "$U" -c "DROP INDEX CONCURRENTLY <invalid index>;"
```

Done when: the query returns no rows and the release is re-run green.

## Database roles

The database is hosted Supabase, so two logins reach it:

| Login | Held in | Used by | Can |
|---|---|---|---|
| `postgres.<ref>` (the migrator, owns every table) | `MIGRATE_DATABASE_URL` in `.env.migrate.production` / `.env.migrate.staging` (mode 600, outside every compose `env_file`) | `prod-migrate.sh`, `staging.sh`, `restore-snapshot.sh`, `staging-sync.sh`, snapshots | DDL |
| `altune_app.<ref>` (BYPASSRLS, owns nothing) | `DATABASE_URL` in `.env.production` / `.env.staging`; the same URL waits in `.env.migrate.<tier>` as `APP_DATABASE_URL` until switched | go-api and its cmd tools | SELECT, INSERT, UPDATE, DELETE; no DDL, no TRUNCATE, no `schema_migrations` |

Migration `052_app_role_grants.sql` gives the app role its rights and default privileges for future tables, turns RLS on, and takes every right from `anon` and `authenticated`. A migration that creates a table needs nothing extra: default privileges cover the app role, and RLS has no policy for the web roles.

```bash
bash deploy/db-role.sh create <staging|production>    # once per tier: role, password from openssl rand, URL stored in .env.migrate.<tier>
bash deploy/db-role.sh switch <staging|production>    # DATABASE_URL -> app role; backup .env.<tier>.bak-approle-<stamp> keeps the old line commented out; then release the running sha
bash deploy/db-role.sh check <staging|production>     # after every deploy: app URL is not the migrator, DDL and TRUNCATE are refused, no web role holds a table privilege
bash deploy/db-role.sh unswitch <staging|production>  # roll back: restore the old DATABASE_URL from the newest backup, then release
bash deploy/db-role.sh drop <staging|production>      # roll back further: revoke and drop the role (refused while the app env uses it)
```

Order on a tier: `create` first (it also applies the grants file, which turns RLS on and revokes the web roles, so it changes the tier's security state as well as adding the role, and is safe before or after the deploy that records 052), then release; `switch` only after a release that carries the `auth_user_ids` queries, then release again. `staging.sh` and `prod-migrate.sh` stop with "not found" until `.env.migrate.<tier>` exists, so run `create` on both tiers before the first deploy that carries them. A snapshot restore brings back the grants and RLS state of its day: run `db-role.sh create <tier>` and `check <tier>` after it.

Rotate the app password with `ALTER ROLE` as the migrator, then update `APP_DATABASE_URL` and the app env together and release. Delete stale `.env.*.bak-*` copies once the new role has run a day.

## Switches

Set in the tier's env file, then release the running sha again so go-api picks it up.

- `ACQUISITION_PAUSED=true` pauses acquisition.
- `DISABLED_JOBS=acquisition prune,stream recovery` turns jobs off. An unknown name fails startup.

## Cookie jar folder

go-api and staging bind-mount the folder `~/altune/cookies`, not a file, so yt-dlp's renewed cookies can replace `cookies.txt` in place. The mount has `create_host_path: false`, so a missing folder fails `compose up` and the deploy script stops there under `set -e`. Run this once on the VM before the first deploy that mounts it, and again if the folder's modes drift:

```bash
cd ~/altune/services/go-api && bash deploy/cookie-jar-dir.sh
```

It moves an old `~/altune/cookies.txt` into the folder. The VM user (uid 1001) is not in go-api's group (gid 1000), so it hands the folder over as root in a go-api image already on the VM (`--pull never --network none -u 0`, no sudo): the folder becomes yours with go-api's group at 2770, so setgid keeps every file go-api writes in that group, and the jar becomes 0660. It then checks as go-api's own user that it can read the jar and replace files in the folder. With no go-api image on the VM it fails rather than pull one. It never prints the jar. Every check runs before anything is touched. `bash deploy/cookie-jar-dir.sh <export>` also drops in a new jar and `--rollback` undoes it; the acquisition-debug skill says how, and why that is the only way to do it.

## VM tools

- `supabase`: logged in, for staging Supabase project ops.
- `deploy/duckdns`: A and TXT records for `*.duckdns.org`; `duckdns help` lists commands. New subdomains are made on the duckdns.org site.
- Script self-tests: `bash deploy/__tests__/<script>.test.sh`.
