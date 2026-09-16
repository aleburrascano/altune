# Staging tier — design (decision spike)

The one real fork with cost trade-offs. This doc names the concrete shape of a staging
tier and recommends one, so the build tasks (#1490–#1494) have a settled target and do
not re-litigate shape. Parent epic: #1488. The operator makes the final call on the
paths that cost money; each recommendation states its cost so it can be approved quickly.

Design settles the *how*; the *what* (merge → staging → smoke gate → promote-on-green) is
inherited from #1488 and not re-opened. Ephemeral per-PR envs, multi-region, and the
app's own auth model are out of scope per the epic.

## Grounded in (real code, verified against the repo)

- One OCI VM `altune.duckdns.org`, repo at `/home/ubuntu/altune`. Docker Compose is the
  unit of deploy: `services/go-api/deploy/compose.prod.yml` (project `name: go-api`).
- The compose stack pins **fixed `container_name`s** (`altune-go-api-blue/green`,
  `altune-overseer`, `altune-redis`, `altune-caddy`) and Caddy **binds host `80:80` and
  `443:443`** (`compose.prod.yml:24-91`). Both facts constrain "just run a second compose
  project": names would collide and the ports are already taken.
- Caddy is a **single site block** for `altune.duckdns.org` with `handle_path /overseer/*`
  → `altune-overseer:8090` and a root `handle` importing `caddy/upstream.conf`
  (`deploy/Caddyfile`). TLS is Caddy-auto (Let's Encrypt, needs port 80 for HTTP-01).
- Blue-green rewrites `deploy/caddy/upstream.conf` and reloads Caddy on **every** deploy
  (`deploy/lib.sh:18-21,72-75`; `deploy/blue-green.sh:28-29`). Anything sharing that
  Caddyfile must not share that upstream file.
- go-api reads its config from `../.env.production` (`compose.prod.yml:8-13`) with a
  hardcoded `OPERATOR_USER_ID` and Supabase anon key. It serves at **root**, not under a
  base path (only overseer has `OVERSEER_BASE_PATH`, per `deploy/RUNBOOK.md:47`).
- Auth + Postgres are one **Supabase project** `ellvexundmgvbbfqbzau`: JWT verified
  against Supabase JWKS, `DATABASE_URL` is the pooler. The overseer operator refresh
  token rotates on every use and is persisted to a volume (`deploy/overseer.sh:50-52`,
  `RUNBOOK.md:50-73`) — the #1471-class bug this epic exists to contain.
- `.github/workflows/deploy-backend.yml` fires on `services/go-api/**` +
  `services/overseer/**` pushes to `main`, gates on the two test workflows, then SSHes in,
  `git reset --hard origin/main`, and runs `blue-green.sh` + `overseer.sh`. **Merge to
  `main` = straight to prod.** Schema migrations are **manual** (warned, not applied —
  `deploy-backend.yml:63-73`).

**The bug class being targeted is runtime/infra, not schema:** volume ownership (#1471),
spent-token bootstrap. These reproduce in a sibling container stack on the same host;
they do not need a separate machine to surface. That framing drives Decisions 1 and 2.

## Decision 1 — Where staging runs

**Recommendation: same VM, its own Compose file as a separate project** (`compose.staging.yml`,
project `staging`), with distinct container names (`altune-staging-go-api-*`,
`altune-staging-redis`, `altune-staging-overseer`), **no host-port binds** (reached only
through the existing prod Caddy over the Docker network), and **memory limits** so a
staging build can't starve prod.

- **Why:** cheapest, no new infra, and it reproduces the exact runtime environment
  (same kernel, same Docker daemon, same volume-ownership semantics) where #1471 lived.
  A separate project keeps staging's blue-green swap, volumes, and lifecycle fully
  independent of prod's.
- **Cost:** isolation is namespace-level only — staging shares the VM's CPU, RAM, and
  disk, so a runaway staging build or a memory spike can degrade prod latency. Mitigated
  by compose `mem_limit`/`cpus` caps and by keeping staging idle except during a deploy.
  A kernel-level or full-host failure still takes both down; acceptable, since the off-box
  uptime check remains the total-down backstop.
- **Rejected — separate VM:** real machine-level isolation, but costs money (a second OCI
  instance) and doubles the ops surface (another host to patch, another SSH target,
  another Caddy/TLS/DNS to keep alive) for a bug class that a same-host sibling stack
  already catches. Revisit only if staging load starts hurting prod.
- **Rejected — ephemeral-on-PR:** most work, and **explicitly out of scope** in #1488.

**Constraint #1490 inherits:** you cannot reuse `compose.prod.yml` with a `-p` flag —
its fixed `container_name`s collide and its Caddy binds 80/443. Staging needs its own
compose file with renamed containers and no port binds.

## Decision 2 — Staging database

**Recommendation: a separate Supabase project for staging.** Supabase's free tier allows
2 active projects, so a dedicated staging project is free. It gives staging its **own auth
realm** (own JWKS, own `auth.users`) and full data isolation from prod.

- **Auth / owner-id:** staging gets its own Supabase auth. The operator creates a staging
  operator account in the staging project; that account's UUID becomes staging's
  `OPERATOR_USER_ID`, `OVERSEER_OWNER_USER_ID`, and the first-boot
  `OVERSEER_GOAPI_REFRESH_TOKEN` seed — all in a staging `.env.staging`, never touching
  prod's `.env.production`.
- **Why:** the smoke gate (#1492) must exercise the **real login + operator-token
  rotation path** — the exact mechanism #1471 broke. That requires a genuine Supabase
  auth realm. A separate project gives one, isolated from prod, so a staging bug that
  spends or corrupts the operator refresh-token chain can only spend *staging's* token.
- **Cost:** a second project's secrets and schema to keep migrated in lockstep with prod
  (migrations are manual today — see the workflow gap below). If the org ever needs a 3rd
  free project, the staging one costs ~$25/mo (Supabase Pro). Free until then.
- **Rejected — separate database/schema on the existing Supabase Postgres:** cheap and
  one project, but **Supabase's auth realm (JWKS, `auth.users`) is per-project, not
  per-database.** A second logical DB still shares prod's auth, so staging would run on
  prod's operator identity and prod's refresh-token chain — a staging bug could spend
  prod's operator token, which is precisely the failure this epic exists to remove.
- **Rejected — throwaway Postgres container:** full isolation and resettable, but it
  provides data with **no Supabase auth realm.** go-api verifies JWTs against Supabase
  JWKS, so staging would still need to point at *some* Supabase project for login, or stub
  auth — and a stubbed login means the smoke gate can't prove the real token path.

## Decision 3 — Routing

**Recommendation: a subdomain served by the existing prod Caddy.** Register a second free
duckdns hostname `altune-staging.duckdns.org` (duckdns allows up to 5 per account) with an
A record to the same VM IP, and add a **second site block** to the one Caddyfile that
reverse-proxies the staging containers. Caddy auto-issues the staging cert.

- **Why:** a subdomain is a **separate origin**, so staging cookies, CORS, and JWT
  audience are fully isolated from prod — no bleed. go-api serves at root, which a
  subdomain honours with zero path rewriting.
- **Caddy config shape** (added to `deploy/Caddyfile`; staging keeps its **own** upstream
  file so prod's blue-green `upstream.conf` rewrites never touch it):

  ```
  altune-staging.duckdns.org {
      redir /overseer /overseer/
      handle_path /overseer/* {
          reverse_proxy altune-staging-overseer:8090
      }
      handle {
          import /etc/caddy/staging-upstream.conf   # written by the staging blue-green
      }
  }
  ```

  Staging reuses the one prod Caddy container (it already owns 80/443; a second Caddy
  can't rebind them and HTTP-01 needs 80). Staging's blue-green writes
  `caddy/staging-upstream.conf` and reloads — distinct from prod's `upstream.conf`, so the
  two deploys never clobber each other.
- **Cost:** a second free duckdns registration + A record, one more site block, and one
  more cert for Caddy to manage. Staging routing shares prod's Caddy process: a malformed
  staging block would fail a config reload — but Caddy validates on reload and **rejects a
  bad config, keeping the running one**, so a broken staging edit cannot take prod's
  routing down.
- **Rejected — path prefix (`altune.duckdns.org/staging/*`):** no new DNS/cert, but it
  shares prod's origin (cookie/CORS/JWT-audience bleed between tiers) and go-api serves at
  **root, not under a base path**, so every API path would need fragile rewriting. The
  isolation a staging tier exists to provide is exactly what a shared origin gives up.

## Decision 4 — Promotion model

**Recommendation: a manual approval gate**, via a GitHub `production` **environment with
required reviewers.** On merge, CI deploys staging and runs the smoke gate; the
promote-to-prod job then **waits for the operator's approval**, shown the green smoke
result, before running the existing `blue-green.sh` against prod.

- **Why:** the epic exists because a *green suite* still shipped a broken prod (#1471). A
  human checkpoint after staging smoke is the backstop for the runtime-bug class that can
  slip an incomplete smoke test — the whole point of the tier. GitHub Environments give
  this for free (protection rule: required reviewers on the prod job); no custom tooling.
- **Cost:** deploys are no longer hands-off — prod waits on a human click, so an approver
  must be available or a deploy sits pending. Accepted deliberately for the first
  iteration; graduate to auto-promote (Decision-4 option A) once the smoke suite has
  earned trust.
- **Rejected — auto-promote on green smoke:** fastest and fully hands-off, but it makes
  **any gap in smoke coverage an immediate prod incident** — re-creating the "green but
  broken" risk the epic removes, just one hop later. Revisit once smoke coverage is proven
  broad enough to trust unattended.

## What B–F build (settled target)

Each build task inherits the picks above; none should re-open shape.

- **#1490 — Provision the staging stack.** `services/go-api/deploy/compose.staging.yml`
  (project `staging`, containers `altune-staging-*`, no host-port binds, `mem_limit`/`cpus`
  caps); a `services/go-api/.env.staging` pointing at the **new staging Supabase project**;
  a staging operator account in that project (its UUID → staging owner ids + refresh-token
  seed); the `altune-staging.duckdns.org` registration + A record; the staging site block
  in `deploy/Caddyfile` importing `caddy/staging-upstream.conf`.
- **#1491 — Deploy to staging first on merge.** Extend `deploy-backend.yml` with a
  `deploy-staging` job (staging blue-green against `compose.staging.yml`) that runs
  **before** any prod step, gated on the existing test workflows.
- **#1492 — Smoke gate against staging.** A `smoke` job hitting `altune-staging.duckdns.org`
  — `/health`, the **real Supabase login + operator-token rotation path**, and key
  endpoints — after the staging deploy. Red blocks promotion.
- **#1493 — Promote to prod on green.** A `deploy-prod` job needing the `smoke` job and the
  `production` environment's **manual approval**, running the existing `blue-green.sh` +
  `overseer.sh` unchanged.
- **#1494 — Runbook.** Document the two-tier flow (merge → staging → smoke → approve →
  prod) in `services/go-api/deploy/RUNBOOK.md`, replacing its current "There is **no
  staging tier**" note.

## Workflow gaps flagged (distinct from any code bug)

- **Migrations stay manual and must now stay in lockstep across two Supabase projects.**
  `deploy-backend.yml:63-73` only *warns* on migration changes; it applies nothing. A
  staging tier is the natural place to apply and prove a migration before prod, but that
  is not wired anywhere. #1490/#1493 should decide whether staging deploy applies staging
  migrations automatically, or the two projects will drift. Out of scope to fix here;
  flagged so the build tasks don't assume the pipeline handles it.
- **The staging Supabase project must not exceed the 2-project free allowance.** If the org
  already runs a second free project elsewhere, staging costs ~$25/mo — an operator
  budget call, not an engineering one.
