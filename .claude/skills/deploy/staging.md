# Staging

Staging is its own Compose project (`staging`, `deploy/compose.staging.yml`) on the prod VM, with no host ports and CPU and memory caps so it can't starve prod. It reaches the world through a second site block on the prod Caddy, with its own upstream file (`deploy/caddy/staging-upstream.conf`), so prod's flips never touch it. It uses a separate Supabase project, so it has its own users and refresh-token chain.

## A fresh staging Supabase project

1. Create the owner with the prod owner's email and password. Its UUID goes in `OVERSEER_OWNER_USER_ID`.
2. Set up overseer's read-only principal on this project.

## Env that must stay staging-scoped

Keep these `.env.staging` keys pointed away from prod, so staging never writes there:

- `FEEDBACK_ENABLED=false`, or `GITEA_ISSUE_REPO=aleburrascano/altune-staging-feedback` (private scratch Gitea repo) with its own token.
- `BEHAVIORAL_CORPUS_PATH` empty.
- `OCI_S3_*` with `AUDIO_KEY_PREFIX=staging/`: a key whose IAM policy only allows writes under `staging/` in prod's bucket (policy `altune-staging-s3-prefix`).
- `MUSICBRAINZ_USER_AGENT`: a real staging contact.

`compose.staging.yml` mounts the shared cookie jar and streamrip config read-only and forces `EVAL_METER_ENABLED=false`. After an env change, recreate the containers it feeds:

```bash
docker compose -f deploy/compose.staging.yml up -d --force-recreate
```

## Staging is disposable

Nothing done on staging is kept. Every night prod's data replaces the matched accounts' staging data, so treat any staging row, playlist or download as temporary. There is no staging to prod path. To keep a song, download it on prod.

## Data from prod

`staging-sync.yml` (Gitea, 08:00 UTC nightly, or on demand from Actions: staging-sync, Run workflow) runs `deploy/staging-sync.sh` on the VM under the same lock as the deploys. It replaces matched accounts' staging data with prod's, one way, in one transaction. Accounts are matched by email. Synced tracks play from prod's keys.

Staging downloads land at `staging/<staging user uuid>/...` and are temporary. After a successful replace, `sweep-staging-audio --execute` runs in the staging container (`docker ps --filter name=^altune-staging-go-api-`). It deletes `staging/` objects that no staging track references and that are over an hour old. A sweep failure only warns.

### Table rules

Every public table has a line in `services/go-api/deploy/staging-sync.tables`: `user`, `child <parent> <fk-col>`, `global`, `reset`, or `skip <reason>`. A new table needs a line, or the sync fails with `FAILED: public.<table> has no rule in staging-sync.tables`, and so does CI. A line for a table that exists on neither tier also fails.

### Schema gate

Before copying, the sync compares the two schemas:

- `SKIPPED: staging is ahead by <versions>`: a deploy is in flight (staging migrated, prod not yet). No action; the next night's run syncs.
- `FAILED: prod has migration <v> that staging lacks`, or `FAILED: staging has migration <v> with no file`: the migration is named in the line.
- `FAILED: <table>.<column> differs: prod <type>, staging <type>`: a column differs between tiers. Fix the schema, not the sync.

## Sign-up lock

Staging has `disable_signup: true`, so nobody can register on their own. To add a tester, create the user by hand in the staging Supabase dashboard (as for the owner above), with the same email as their prod account so the sync matches it.

## Auth settings as code

`supabase/auth/base.json` holds the settings both tiers share. `prod.json` and `staging.json` hold the per-tier keys (the sign-up lock, `uri_allow_list`). To change a setting, edit the JSON, then run both:

```bash
scripts/supabase-auth.sh apply prod
scripts/supabase-auth.sh apply staging
```

`diff <tier>` shows the drift without changing anything. It needs a Supabase access token (`SUPABASE_ACCESS_TOKEN`, or `~/.supabase/access-token`). Secrets (provider secrets, SMTP password, hook secrets) are set in the dashboard, never in the repo; `apply` reports one as `unset` but cannot set it.

The nightly job runs `diff` for both tiers after the sync and goes red on drift or a `NEW KEY` (a live setting the JSON doesn't name). Add the key to the JSON, or to `$ignore` in `base.json`, to turn it green.

## Rollback

Rolling staging back alone to a script version that still runs the staging-to-prod copy step turns the sync red with "unknown command". No data is lost.
