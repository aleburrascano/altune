# Overseer deploy

Overseer ships with go-api through the pipeline in the
[deploy runbook](../../../services/go-api/deploy/RUNBOOK.md).
[`overseer.sh`](../../../services/go-api/deploy/overseer.sh) rebuilds and recreates the one
container (a short `/overseer` blip; go-api traffic is untouched), fixes a root-owned data
volume, and fails the deploy if overseer is unhealthy or logs a token failure. Caddy routes
`/overseer/*` to `altune-overseer:8090`.

## Env

`overseer.sh` refuses to deploy without these in `.env.production`:

- `OVERSEER_OWNER_USER_ID`: the owner's Supabase user UUID, the only login allowed.
- `OVERSEER_SUPABASE_URL`, `OVERSEER_SUPABASE_ANON_KEY`: public, served to the SPA at
  `/config.json`. Tokens must carry `iss` `{OVERSEER_SUPABASE_URL}/auth/v1`, so the URL must
  be the host Supabase auth stamps on its tokens; anything else rejects every login with 401.

Read by the app but not gated (a missing one shows its buckets as `source_down`):

- `OVERSEER_GOAPI_URL`: `http://altune-caddy:8081` on prod, `:8082` on staging.
- `OVERSEER_GOAPI_READONLY_EMAIL`, `OVERSEER_GOAPI_READONLY_PASSWORD`: the read-only
  principal's sign-in. Both are needed for self-healing.
- `OVERSEER_GOAPI_READONLY_REFRESH_TOKEN`: optional seed; not needed when the pair is set.
  `OVERSEER_GOAPI_REFRESH_TOKEN` and `OVERSEER_GOAPI_TOKEN` are ignored (logged as
  `ignored_var=...`).
- `OVERSEER_BASE_PATH=/overseer`, `OVERSEER_OCI_ENABLED` (cost bucket, see below).
- `OVERSEER_OWNER_TOKEN` is not used.

## The read-only principal

Overseer reads `/observe/*` as one Supabase user. go-api answers 403 to every other
subject, and to everyone when `OVERSEER_PRINCIPAL_ID` is unset. Once per Supabase project:

1. Create a dedicated Supabase user for overseer. It must not be the owner.
2. Put its UUID in go-api's `OVERSEER_PRINCIPAL_ID`. go-api refuses to start on a non-UUID.
3. Put its email and password in `OVERSEER_GOAPI_READONLY_EMAIL` and
   `OVERSEER_GOAPI_READONLY_PASSWORD`.

## Refresh token

Supabase rotates the refresh token on every use. Overseer persists it to
`/var/lib/overseer/readonly_refresh_token` on the `overseer-data` volume (mode 600, guarded
by `readonly_refresh_token.lock`), so a restart resumes the chain. When a refresh returns
`400` and the file holds nothing newer, overseer signs in again with the password grant
and persists the new token. No manual reseed.

| Log line | Meaning |
|---|---|
| `read-only account signed in again with the password grant` | Chain healed. |
| `read-only token refresh failed at password_grant` | Email/password wrong or rate-limited (`400`/`429`); backs off. Check the pair. Fails the deploy and smoke gate. |
| `persisting rotated refresh token failed` | Volume not writable; the token lives in memory until restart. Fix the volume. |
| `refresh token file unusable, continuing unpersisted` | Token dir or file unreadable or unlockable; overseer runs from the env seed or password grant. |

Rotate the password: change it in Supabase, update `OVERSEER_GOAPI_READONLY_PASSWORD`, run
`bash deploy/overseer.sh`. The live chain keeps working; the new password is used the next
time the chain dies.

## Reading go-api through Caddy

Overseer reads go-api over the Docker network through internal-only Caddy listeners that
import the same upstream file as the public site, so they follow every blue/green flip and
rollback. Neither port is published to the host.

| Listener | Imports | Serves |
|---|---|---|
| `altune-caddy:8081` | `upstream.conf` | prod go-api |
| `altune-caddy:8082` | `staging-upstream.conf` | staging go-api |

Check and fix, from `services/go-api`:

```bash
docker exec altune-overseer wget -q -O - http://altune-caddy:8081/health
docker exec altune-staging-overseer wget -q -O - http://altune-caddy:8082/health
docker compose -f deploy/compose.prod.yml up -d --force-recreate caddy   # if refused; short 80/443 blip
docker compose -f deploy/compose.staging.yml up -d --force-recreate overseer   # after a staging env change
```

If a listener can't be fixed, set `OVERSEER_GOAPI_URL` to the tier's public URL and recreate
overseer.

## OCI cost access

The spend half of the cost bucket reads OCI's usage API as the instance principal. Without
a grant it logs `usage-api denied access (HTTP 404 NotAuthorizedOrNotFound)` hourly and
shows `STALE`. A human grants it once in the OCI console:

1. Identity & Security, Domains, Dynamic Groups: a dynamic group matching the prod instance,
   for example `instance.compartment.id = '<compartment-ocid>'`.
2. Identity & Security, Policies, in the root compartment:
   `Allow dynamic-group <dynamic-group> to read usage-report in tenancy`.
3. After the next hourly refresh (or an overseer restart), the Cost panel shows a live
   figure and `docker compose -f deploy/compose.prod.yml logs overseer | grep "usage-api denied"`
   shows nothing new.

Self-test for the deploy script: `bash services/go-api/deploy/overseer_test.sh`.
