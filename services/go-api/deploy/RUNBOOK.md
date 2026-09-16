# Deploy runbook

Operational deploy steps for the OCI prod VM (`altune.duckdns.org`, repo at
`/home/ubuntu/altune`). This lives next to the deploy machinery on purpose: the
release chair (`ship`) reads it here; no other context needs it.

There is **no staging tier**. Merge to `main` auto-deploys `go-api` **and**
`overseer` to prod.

## go-api (automated)

CI handles it: `.github/workflows/deploy-backend.yml` triggers on
`services/go-api/**` (and `services/overseer/**`) pushes to `main`, SSHes in, and
runs `services/go-api/deploy/blue-green.sh` (builds + flips
`go-api-blue`/`go-api-green`). Nothing manual.

## overseer (automated — since #1470)

The same SSH step also runs `services/go-api/deploy/overseer.sh` on every deploy:
it verifies the required `OVERSEER_*` vars in `.env.production` (fails the deploy
loudly if any is missing), then rebuilds + recreates the single `altune-overseer`
container — no blue-green (a brief `/overseer` blip; go-api traffic is untouched).

Full runbook — required env, the refresh-token gotcha, smoke test, and the manual
fallback — lives in **`docs/features/overseer/deploy.md`**. Keep the two in sync.
