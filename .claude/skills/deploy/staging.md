# Staging

Staging is its own Compose project (`staging`, `deploy/compose.staging.yml`) on the prod VM, with no host ports and CPU and memory caps so it can't starve prod. It reaches the world through a second site block on the prod Caddy, with its own upstream file (`deploy/caddy/staging-upstream.conf`), so prod's flips never touch it. It uses a separate Supabase project, so it has its own users and refresh-token chain.

## A fresh staging Supabase project

1. Create the owner with the prod owner's email and password. Its UUID goes in `OVERSEER_OWNER_USER_ID`.
2. Set up overseer's read-only principal on this project.

## Env that must stay staging-scoped

Keep these `.env.staging` keys pointed away from prod, so staging never writes there:

- `FEEDBACK_ENABLED=false`, or a scratch `GITHUB_ISSUE_REPO` and token.
- `BEHAVIORAL_CORPUS_PATH` empty.
- `OCI_S3_*`: a key that is read-only on prod's bucket.
- `MUSICBRAINZ_USER_AGENT`: a real staging contact.

`compose.staging.yml` mounts the shared cookie jar and streamrip config read-only and forces `EVAL_METER_ENABLED=false`. After an env change, recreate the containers it feeds:

```bash
docker compose -f deploy/compose.staging.yml up -d --force-recreate
```

## Data from prod

`staging-sync.yml` runs `deploy/staging-sync.sh` nightly, or on demand from Actions (staging-sync, Run workflow). It copies matched accounts' prod data one way, in one transaction, under the same VM lock as the deploys. Synced tracks play through the read-only bucket key; new downloads on staging fail.
