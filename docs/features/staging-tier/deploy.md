# Staging tier — operating the deploy flow

The authoritative, kept-in-sync operator runbook lives next to the deploy machinery:
**`services/go-api/deploy/RUNBOOK.md`**. This page is the feature-doc entry point; it
does not restate the steps (one source of truth, no drift).

The runbook covers the two-tier flow built by epic #1488:

- **The pipeline** — `merge → test + test-overseer → deploy-staging → smoke-staging →
  deploy-prod` (`.github/workflows/deploy-backend.yml`). Prod is touched only after a
  green staging smoke **and** a manual approval.
- **Deploy to staging** — `deploy/staging.sh` applies staging migrations (via a
  `schema_migrations` tracker) and recreates only the `altune-staging-*` stack.
- **The smoke gate** — `deploy/smoke.sh` against `https://altune-staging.duckdns.org`.
- **Promote to prod** — approve/deny the `production` environment in GitHub Actions.
- **Roll back** each tier — prod `deploy/rollback.sh`; staging redeploy/recreate.
- **The migration-lockstep asymmetry** — staging auto-applies migrations; **prod
  migrations stay manual** (the deploy-prod step only warns). The exact by-hand `psql`
  step is in the runbook. See also the "Workflow gaps flagged" note in `design.md`.
- **The web tier** — `.github/workflows/deploy-web.yml` exports `apps/mobile` for web on
  every push to `main` under `apps/mobile/**`, ships it to
  `/home/ubuntu/altune-web/staging/releases/<sha>`, flips the relative `current` link
  (`deploy/web-release.sh`), and smoke-tests staging. The shared Caddy serves a file from
  that release when one matches the path and hands everything else to go-api as before.
  Rollback reruns `web-release.sh staging <previous-sha>`. Staging only; prod is a later
  slice of the web-app epic (`docs/features/web-app/plan.md`).
- **Staging tier facts** — entrypoint, the separate Supabase project
  (`ijyjoyxhwmbmriwzazbx`), `.env.staging` secrets on the VM, container names, owner
  bootstrap for dashboard access, and the `supabase` / `duckdns` CLIs.

DNS: the `duckdns` CLI (commands, token resolution, the no-create-subdomain
limitation) is documented in `docs/features/staging-tier/dns.md`.

Design rationale for every choice above (why same-VM, why a separate Supabase project,
why a manual approval gate): `docs/features/staging-tier/design.md`.

Isolation hardening — which `.env.staging` keys must be staging-scoped so a staging run
can't touch prod (OCI_S3, alert/webhook, feedback GitHub token, etc.), plus the read-only
shared mounts: `docs/features/staging-tier/hardening.md`.
