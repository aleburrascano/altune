# Staging

Staging is its own Compose project (`staging`, `deploy/compose.staging.yml`) on the prod VM, with no host ports and CPU and memory caps so it can't starve prod. It reaches the world through a second site block on the prod Caddy, with its own upstream file (`deploy/caddy/staging-upstream.conf`), so prod's flips never touch it. It uses a separate Supabase project, so it has its own users and refresh-token chain.

## A fresh staging Supabase project

1. Create the owner with the prod owner's email and password. Its UUID goes in `OVERSEER_OWNER_USER_ID`.
2. Set up overseer's read-only principal on this project.

## Env that must stay staging-scoped

Keep these `.env.staging` keys pointed away from prod, so staging never writes there:

- `FEEDBACK_ENABLED=false`, or a scratch `GITHUB_ISSUE_REPO` and token.
- `BEHAVIORAL_CORPUS_PATH` empty.
- `OCI_S3_*` with `AUDIO_KEY_PREFIX=staging/`: a key whose IAM policy only allows writes under `staging/` in prod's bucket (policy `altune-staging-s3-prefix`).
- `MUSICBRAINZ_USER_AGENT`: a real staging contact.

`compose.staging.yml` mounts the shared cookie jar and streamrip config read-only and forces `EVAL_METER_ENABLED=false`. After an env change, recreate the containers it feeds:

```bash
docker compose -f deploy/compose.staging.yml up -d --force-recreate
```

## Data from prod

`staging-sync.yml` runs `deploy/staging-sync.sh` nightly, or on demand from Actions (staging-sync, Run workflow). It copies matched accounts' prod data one way, in one transaction, under the same VM lock as the deploys. Synced tracks play from prod's keys, and staging downloads land at `staging/<staging user uuid>/...`.

Prod is not read-only overall. Before the replace, the script runs `promote-staging --execute` in the running prod go-api container (`docker ps --filter name=^altune-go-api-`). It INSERT-only lands every track a user kept on staging (`acquisition_status = 'ready'`, `audio_ref LIKE 'staging/%'`) into prod under the matched account, with a server-side copy onto the prod key. It never updates or deletes a prod row, and it skips any track whose `(user_id, dedup_key)` or object already exists in prod. A promote failure aborts the sync, so an unpromoted song is never wiped.

After a successful replace, `sweep-staging-audio --execute` runs in the staging container (`docker ps --filter name=^altune-staging-go-api-`). It deletes `staging/` objects that no staging track references and that are over an hour old. A sweep failure only warns.
