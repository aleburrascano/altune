# Staging tier design

Staging proves a build on real infrastructure before prod. Operating it:
[deploy runbook](../../../services/go-api/deploy/RUNBOOK.md).

- Same VM as prod, as its own Compose project
  ([`compose.staging.yml`](../../../services/go-api/deploy/compose.staging.yml), project
  `staging`, containers `altune-staging-*`). No host ports, `mem_limit`/`cpus` caps so a
  staging build can't starve prod. It reproduces prod's kernel, Docker daemon and volume
  semantics, which is where the runtime bugs it targets live. `compose.prod.yml` can't be
  reused with `-p`: its container names are fixed and its Caddy binds 80/443.
- A separate Supabase project. Supabase auth (JWKS, `auth.users`) is per project, so only a
  separate project gives staging its own login and refresh-token chain; a staging bug can
  only spend staging's tokens.
- Its own origin, `altune-staging.duckdns.org`, as a second site block on the prod Caddy.
  Separate cookies, CORS and JWT audience; go-api serves at root with no path rewriting.
  Staging has its own upstream file (`caddy/staging-upstream.conf`), so prod's blue-green
  rewrites never touch it. Caddy rejects a bad config on reload and keeps the running one.
- Promotion needs a human: a required reviewer on the GitHub `production` environment,
  after a green staging smoke. Auto-promote waits until smoke coverage is trusted.

Cost: staging shares the VM's CPU, RAM and disk, and a host failure takes down both tiers.
The staging Supabase project is free while the org stays within the free project allowance.
