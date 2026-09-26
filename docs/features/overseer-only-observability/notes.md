# Capability: Overseer as the only observability platform

Epic #2798 (closed). Plan: `docs/features/overseer-only-observability/plan.md`. This note is the
epic's memory — what now works, how to reach it, and the invariants it holds — confirmed live on
prod at epic-close.

## What now works

The owner watches Altune through Overseer and nothing else. go-api serves a small, purpose-built,
read-only surface for Overseer under `internal/observe`, mounted at `/observe/*`; Mission
Control's `/admin` backend — every route, `internal/admin/handler`, `admin_wiring.go`, the
operator principals — is gone.

- **`/observe/*` routes (GET only)**: `health`, `metrics/live`, `eval`, `acquisition`,
  `quality/discography`, plus the `events/stream` and `logs/stream` SSE feeds
  (`services/go-api/internal/observe/handler/`: `health.go`, `reads.go`, `metrics_live.go`,
  `eval.go`, `acquisition.go`, `quality.go`, `streams.go`, `sse.go`). Response bodies are
  byte-compatible with the old `/admin` equivalents, so Overseer's hand-mirrored DTOs decode
  unchanged; a go-api contract test (`reads_contract_test.go`, `streams_contract_test.go`) pins
  that against Overseer's own fixtures.
- **One gate, fail-closed**: `observeHandler.Gate(OVERSEER_PRINCIPAL_ID)` behind
  `authMiddleware`, wired once in `internal/app/observe_wiring.go` (`mountObserve`). No token →
  401; any other principal, or an empty `OVERSEER_PRINCIPAL_ID`, → 403
  (`observe.principal_required`). The old operator/read-only split is gone — one principal, one
  audience, since there are no write routes left.
- **Producers moved in**: `eventtap` and `evalmeter` now live under `internal/observe/`, wired
  from `observe_wiring.go` instead of the old `admin_wiring.go`.
- **Kill switches became startup settings**: `ACQUISITION_PAUSED` (bool, scheduler starts paused)
  and `DISABLED_JOBS` (comma-separated job names, disabled at startup; an unknown name fails
  config validation and startup). `EVAL_METER_ENABLED` is unchanged. The old runtime
  `Pause`/`Resume`/`SetJobEnabled` POSTs are gone with `/admin`; pausing acquisition is now an env
  change + restart, not a live button.
- **Metrics rollup removed**: the `startMetricsRollup` job, `NewPgxMetricsRollup`,
  `ports.MetricsRollupStore` and the `discovery_metrics` table are gone (migration 025, forward
  only, irreversible — confirmed by the owner after `cmd/discoveryeval/report.go` was found as a
  second writer). The nightly discovery-eval schedule (`discovery-eval-nightly.yml`) is
  paused; the nightly eval is manual-only now (`cmd/discoveryeval`, run by hand).
- **Guards that stay**: a go-api router test fails on any route mounted under `/admin`, and an
  Overseer test fails if any `goapi` client path starts with `/admin` — so a regression toward
  Mission Control's surface fails CI, not just review.
- **Overseer's security bucket** re-targets its gate probe: `buckets/security/suite.go`'s
  `observe-gate` check now hits unauthenticated `GET /observe/health` and expects 401 (renamed
  from the old admin-operator probe).

## How to reach it

- **The owner**: unchanged — Overseer's owner-guarded shell (`GET /`) at the Overseer host; every
  bucket (backend-perf, cost, domain quality, logs, reliability, security, usage) reads
  `/observe/*` instead of `/admin/*`, same panels, no gap through the switch.
- **The service account**: Overseer's goapi client attaches a bearer whose subject must equal
  `OVERSEER_PRINCIPAL_ID` (a Supabase user id) set on both go-api deploys
  (`.env.production`, `.env.staging`); the client's path constants and SSE consumers point at
  `/observe/health`, `/observe/metrics/live`, `/observe/eval`, `/observe/acquisition`,
  `/observe/quality/discography`, `/observe/events/stream`, `/observe/logs/stream`.
- **Direct curl** (diagnosing the gate itself): `GET /observe/health` with no token → 401; with a
  token whose subject isn't `OVERSEER_PRINCIPAL_ID` → 403; with the Overseer service-account token
  → 200, same body shape `/admin/health` used to return.
- Per-note reach instructions for each bucket that reads go-api are in the eleven capability notes
  fixed alongside this one (see Cross-references below) — they now point at `/observe/*`, not the
  deleted `/admin/*`.

## Where it runs

- **Deploy**: this repo has one tier below prod (staging), no separate promote step beyond
  `approve-prod`. Merge to `main` → `deploy-backend.yml` builds and ships go-api and Overseer to
  staging, smoke-tests, gates on `approve-prod`, then blue-green deploys to the OCI prod VM.
- **Prod, proven live**: run `36221000752` deployed commit `8dc17e4c` (all jobs green: test-overseer,
  test, deploy-staging, smoke-staging, approve-prod, deploy-prod). Migration 025 (the irreversible
  rollup-table drop) shipped earlier in commit `717431e7`. On prod: `GET /admin/health` → 404 (the
  tree is gone), `GET /observe/health` → 401 with no token, `GET /health` → 200 (open probe,
  unaffected), and Overseer's `/observe` reads → 200.
- **Staging**: proved live the same way before prod promotion.
- **Observed by**: Overseer itself — every bucket keeps collecting through the deploy; the
  security bucket's `observe-gate` check is the automated proof the gate is still enforced.

## Must-holds it keeps (QA green on all 12)

- Every `/observe/*` route: 401 with no token, 403 for any principal other than
  `OVERSEER_PRINCIPAL_ID` (including an empty configured value), 405 for any non-GET method.
- Every `/observe/*` response carries `Cache-Control: no-store` (streams: `no-cache`) and
  `X-Content-Type-Options: nosniff`.
- Every `/observe/*` JSON body decodes into Overseer's current mirrored DTO with no field lost
  (contract test, shared fixtures with the old `/admin` equivalents).
- `/observe/events/stream` never carries a raw user id; `/observe/logs/stream` serves only
  records that passed the logging redactor; every record on both streams carries its `corr_id`
  when the originating request had one.
- `/observe/metrics/live` serves bucket counts, never precomputed percentiles; route keys are
  templates, never raw paths or ids, bounded by the reqmetrics cardinality cap.
- SSE streams end at token expiry, at shutdown, and at 15 minutes; a 17th subscriber gets 429.
- `ACQUISITION_PAUSED=true` → the scheduler reports paused at startup; `DISABLED_JOBS=x` with an
  unknown `x` → startup fails, naming `x`.
- No route was ever added or modified under `/admin` during the build (only ever deleted); after
  the delete slice, the go-api router has no `/admin` route at all; after cleanup, no Go package
  path contains `internal/admin/handler` and config loads with no `OPERATOR_*` variable set; no
  Overseer goapi request path starts with `/admin`.

## What broke before (nothing carried into this close)

QA ran all 12 must-holds green with no interaction bugs or usable-gate gaps surfaced against this
epic. The one open risk the plan flagged — the rollup table having a second production writer
(`cmd/discoveryeval/report.go`) — was found before the drop, resolved by owner re-confirmation and
pausing the nightly schedule (#2806), and is not a live gap: the nightly eval is manual-only going
forward, by design, not a regression.

## Cross-references

- Sub-issues (all closed): #2799–#2810.
- Capability notes fixed alongside this close (#2933 — they pointed at the deleted `/admin`
  routes): `docs/features/backend-perf/notes.md`, `docs/features/cost/notes.md`,
  `docs/features/cost-enabler/notes.md`, `docs/features/domain-quality/notes.md`,
  `docs/features/domain-quality-deep/notes.md`, `docs/features/domain-quality-deeper/notes.md`,
  `docs/features/logs/notes.md`, `docs/features/metrics-enabler/notes.md`,
  `docs/features/reliability/notes.md`, `docs/features/security/notes.md`,
  `docs/features/usage/notes.md`. Historical design docs (`plan.md`, `docs/drafts/`) were left
  alone — they are history, not live docs.
</content>
