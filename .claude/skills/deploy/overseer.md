# Overseer

`deploy/overseer.sh` ships overseer with go-api: it recreates the one container (a short `/overseer` blip), fixes a root-owned data volume, and fails the release on a missing `OVERSEER_*` var, an unhealthy container or a token failure in its logs. Caddy routes `/overseer/*` to it.

## The read-only principal

Overseer reads go-api's `/observe/*` as one Supabase user. go-api answers 403 to every other subject, and to everyone while `OVERSEER_PRINCIPAL_ID` is unset. Once per Supabase project:

1. Create a dedicated user. It must not be the owner.
2. Put its UUID in go-api's `OVERSEER_PRINCIPAL_ID`.
3. Put its email and password in `OVERSEER_GOAPI_READONLY_EMAIL` and `OVERSEER_GOAPI_READONLY_PASSWORD`.

`OVERSEER_SUPABASE_URL` must be the host Supabase stamps into its tokens' `iss`; any other value rejects every login with 401.

Rotate the password: change it in Supabase, update `OVERSEER_GOAPI_READONLY_PASSWORD`, run `bash deploy/overseer.sh`.

## Token log lines

Overseer persists the rotating refresh token on its data volume and signs in again with the password when a refresh fails, so it needs no manual reseed.

| Log line | Meaning |
|---|---|
| `read-only account signed in again with the password grant` | The chain healed. |
| `read-only token refresh failed at password_grant` | The email or password is wrong, or rate-limited. Fix the pair. Fails the release and smoke. |
| `persisting rotated refresh token failed` | The volume is not writable. Fix the volume. |
| `refresh token file unusable, continuing unpersisted` | The token file is unreadable or unlockable; overseer runs from the password grant. |

## `source_down` buckets

Overseer reaches go-api through Caddy listeners internal to the Docker network, which follow every flip: `altune-caddy:8081` for prod and `altune-caddy:8082` for staging (`OVERSEER_GOAPI_URL`).

```bash
docker exec altune-overseer wget -q -O - http://altune-caddy:8081/health
docker exec altune-staging-overseer wget -q -O - http://altune-caddy:8082/health
docker compose -f deploy/compose.prod.yml up -d --force-recreate caddy
docker compose -f deploy/compose.staging.yml up -d --force-recreate overseer
```

Recreate Caddy when a listener refuses (a short 80/443 blip), and staging overseer after a `.env.staging` change. If a listener stays down, set `OVERSEER_GOAPI_URL` to the tier's public URL and recreate overseer.

## `STALE` cost

The spend figure reads OCI's usage API as the instance principal. Without a grant, overseer logs `usage-api denied access (HTTP 404 NotAuthorizedOrNotFound)` hourly. The operator grants it once in the OCI console:

1. A dynamic group matching the prod instance, e.g. `instance.compartment.id = '<compartment-ocid>'`.
2. A root-compartment policy: `Allow dynamic-group <dynamic-group> to read usage-report in tenancy`.

After the next hourly refresh or an overseer restart, the Cost panel shows a live figure.

## Proof

Sign in at `/overseer/` and see live buckets. Only a real browser login proves owner auth. Without a session, `https://altune.duckdns.org/overseer/api/buckets` answers 401.
