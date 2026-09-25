# Overseer as the only observability platform

## Outcome

The owner watches Altune through Overseer and nothing else. Today Overseer still reads go-api
through Mission Control's backend: the `/admin/*` routes served by `internal/admin`, behind the
operator/read-only admin principals. Done looks like: go-api exposes a small surface built for
Overseer, Overseer reads only that, and `internal/admin`, every `/admin` route, the admin
principals and the leftover Mission Control config, lint rules and docs are gone. Overseer keeps
every panel it has today, with no gap at any deploy along the way.

## Scope

### In

**New go-api module `internal/observe`, mounted at `/observe/*` (read-only, GET only)**
- `GET /observe/health`, `/observe/metrics/live`, `/observe/eval`, `/observe/acquisition`,
  `/observe/quality/discography`, `/observe/events/stream` (SSE), `/observe/logs/stream` (SSE).
  Response bodies byte-compatible with today's `/admin` equivalents, so Overseer's mirrored DTOs
  decode unchanged.
- Carries over the hardening the admin routes earned: `Cache-Control: no-store` + `nosniff` on
  every route, SSE subscriber caps (429), per-write idle deadline + keepalive, 15-min stream
  lifetime, stream ends at token expiry and on shutdown, audit log line per stream open, user id
  digested on the event stream, coded JSON errors.
- Producers move in with it: `eventtap` → `internal/observe/eventtap`, `evalmeter` →
  `internal/observe/evalmeter`. The composition root (`internal/app`) keeps adapting app-owned
  types (health probe, eval runner, live metrics) onto the module's DTOs, as `admin_wiring.go`
  does today, in a new `observe_wiring.go`.
- Gate: one principal, `OVERSEER_PRINCIPAL_ID` (a Supabase user id). Everyone else gets 403 and
  no token gets 401. Until the final slice, an unset `OVERSEER_PRINCIPAL_ID` falls back to
  `OPERATOR_READONLY_USER_ID` so no deploy needs the env change first.
- depguard `observe-boundary` + `observe-leaf-boundary` rules, deny entries in the other modules'
  rules and `shared-bottom`, `.claude/module-tiers.tsv` line, `doc.go` seam map.

**Overseer switches to `/observe/*`**
- `services/overseer/internal/goapi` path constants: health, metrics/live (both readers),
  eval, acquisition, quality/discography, events stream, logs stream. Their tests follow.
- Bucket `Op` strings and tests (`backendperf`, `cost`, `reliability`, `poller`,
  `degraded_precedence`, `domainquality`).
- Security suite's operator-gate probe (`buckets/security/suite.go:55-58`) re-targets an
  unauthenticated `GET /observe/health` expecting 401. Check name and web fixtures
  (`security.panel*.test.tsx`) renamed from "admin-operator" to "observe-gate".
- Logs consumer gains the same path option the events consumer has.

**Kill switches become startup settings**
- `ACQUISITION_PAUSED` (bool): the acquisition scheduler starts paused.
- `DISABLED_JOBS` (comma-separated job names): those jobs start disabled; an unknown name fails
  config validation at startup.
- `EVAL_METER_ENABLED` already exists and stays the eval switch.
- Runtime `Pause`/`Resume`/`SetJobEnabled` and the `adminJobs` adapter go with the `/admin`
  tree (slice 3); nothing else calls them.
- `.env.example`, staging example, RUNBOOK section "how to pause acquisition / disable a job".

**Metrics-history rollup removed**
- Delete the `startMetricsRollup` job (`background_jobs.go:171`), `NewPgxMetricsRollup` and
  `ports.MetricsRollupStore`, and a forward migration dropping the rollup table. (Its only
  reader, `GET /admin/metrics`, goes with the `/admin` tree.)

**Delete the `/admin` tree in one cut, right after Overseer switches**
- Every `/admin` route at once: the reads Overseer used, the kill-switch POSTs, `/admin/jobs*`,
  `/admin/metrics`. No slice edits or adds an `/admin` route; `/admin` is only ever deleted.
- `internal/admin/handler`, `admin_wiring.go`, `mountAdmin`, `adminJobs`, the router guard
  test. If ntfy's `/alerts` routes are still there, they go too (the alert monitor itself stays
  for the other chat).

**Delete what's left of Mission Control**
- `internal/admin` package directory (once `alert` is gone with the ntfy work).
- `OPERATOR_USER_ID` and `OPERATOR_READONLY_USER_ID`: config fields, validation (startup no
  longer requires an operator id), tests, `.env.example`, `.env.development`, staging example,
  `compose.prod.yml:11-14`, and the fallback added above.
- Seams renamed off "admin": `ports.AdminActivity` / `EmitAdminOnly` and
  `WithSearchAdminActivity` / `WithRecordEventAdminActivity` become an observe-neutral name.
- depguard `admin-boundary`, `admin-leaf-boundary` and admin deny entries; module-tiers line;
  `.github/labels.yml` `area:admin`; RUNBOOK read-only-principal section rewritten for
  `OVERSEER_PRINCIPAL_ID`; `docs/diagrams/{system,telemetry}.md`; the ARCHITECTURE.md and
  comment references the explore pass listed (`event_repo_retention.go:12`, `app/doc.go`,
  `jobs.go`, `routes.go`, `auth_wiring.go`, catalog/playback `doc.go`, `reqmetrics`,
  `providermetrics`); `uptime-check.yml` "Mission Control" header.
- A guard that stays: a test in go-api that walks the router and fails on any `/admin` route, and
  one in Overseer that fails if any goapi path starts with `/admin`.

### Out

- `internal/admin/alert` and `/alerts`: being removed with ntfy in another chat. The final
  cleanup slice waits for that to land (see Risks).
- Giving Overseer write controls: Overseer is observe-only by design; switches became startup
  settings instead.
- Overseer UI/panel changes beyond renamed check labels: a different job (overseer-revamp).
- `docs/features/*` historical design docs: history, not live docs.

## Risks

- **Data deletion.** The migration dropping the metrics rollup table is irreversible. Accepted by
  the owner; it runs in its own slice so it can be reviewed alone.
- **Deploy ordering across two services.** Overseer must not switch before `/observe` is live in
  the go-api it points at (staging and prod), and `/admin` must not be deleted before Overseer's
  switch is deployed. Order: go-api `/observe` ships → Overseer switches and ships → `/admin`
  deleted. `test-overseer.yml` only runs on `services/overseer/**`, so go-api slices don't
  exercise Overseer's contract tests; the go-api slice must add its own contract test for the
  response shapes.
- **Env change on the servers.** `OVERSEER_PRINCIPAL_ID` must be set in `.env.production` and
  `.env.staging` before slice 5 removes the `OPERATOR_READONLY_USER_ID` fallback. Owner
  does this at ship; the final slice's ship step checks it.
- **Losing a runtime brake.** After the switch change, pausing acquisition means edit env +
  restart, not an instant button. Accepted.
- **Security.** The event and log streams carry user activity and diagnostic text. The new gate
  must be at least as strict as today's: a single principal, GET only, the same redaction and
  digests. Mistakes here expose user data.
- **Blocked on other work.** Removing the `internal/admin` directory needs the ntfy/alert
  removal merged first. Removing every `/admin` route does not (slice 3).

## Build

New boundary, so decided through the lenses:

- **Boundaries:** one new peer module `internal/observe` owns the read surface and its two
  producers (eventtap, evalmeter). *Over* folding each endpoint into its owning module (health →
  app, discography → discovery, …): the producers are cross-cutting, so scattering them spreads
  the gate and multiplies depguard rules. *Over* keeping `/admin` paths in a renamed package:
  rejected by the owner, since it keeps Mission Control's surface alive under a new name.
- **Data & state:** no new store. Live data stays in memory (log ring, event feed, expvar);
  discography quality reads the existing event store. The rollup table is dropped.
- **Coupling:** Overseer keeps hand-mirrored DTOs (no shared module), so bodies must stay
  byte-compatible, pinned by a go-api contract test using fixtures copied from Overseer's
  decoders.
- **Auth:** one principal via `auth.Middleware` + a single-id gate. *Over* keeping two principals
  (operator + read-only): with no write routes left, the operator role has no job.
- **Pull endpoints over log shipping** (owner's call, checked against the observability lexicon):
  the book takes no side on transport; it asks for a narrow stack with one place to look, and
  warns that logs alone make trend analysis hard. So metrics stay metrics (served), events and
  logs stay streams, and Overseer is the one place to look.
- **Metrics stay raw:** `/observe/metrics/live` keeps serving cumulative per-route histogram
  buckets + count + sum, keyed by route template with the existing cardinality cap; Overseer
  computes rates and percentiles. *Over* go-api precomputing p95s: loses the ability to
  aggregate across windows (lexicon: metrics-as-time-series, "averages where percentiles or
  histograms were needed").
- **One principal, not split by sensitivity:** the lexicon suggests restricting raw logs/events
  more than aggregates; with one owner and one service account a second principal adds a
  credential without a second audience. Redaction before emit is the real control and stays.
- **Lifetimes (a month out):** SSE streams stay capped (subscribers, 15-min lifetime, token
  expiry), so a stuck Overseer can't pin goroutines. The fallback env read is removed in the
  final slice so it can't linger.

```mermaid
flowchart LR
  subgraph go-api
    app[internal/app composition root] --> obs[internal/observe<br/>handler · eventtap · evalmeter]
    obs --> routes["/observe/* GET only<br/>gate: OVERSEER_PRINCIPAL_ID"]
  end
  overseer[services/overseer goapi client] -->|JWT, SSE + REST| routes
  overseer -->|public| health[/health]
```

Slices, in merge and deploy order:
1. `internal/observe` + `/observe/*`; producers moved; principal with fallback; contract test.
   Also: `ACQUISITION_PAUSED` / `DISABLED_JOBS` startup settings (independent, can run in
   parallel).
2. Overseer switches to `/observe/*`; security probe re-target; `/admin` guard test in Overseer.
3. Delete the entire `/admin` tree in one cut: every route, `internal/admin/handler`,
   `admin_wiring.go`, runtime toggles; router guard test that fails on any `/admin` route.
   (Waits only on slice 2 being deployed.)
4. Metrics rollup: delete job + store; drop-table migration.
5. Cleanup: `internal/admin` directory, operator principals + fallback, seams renamed, lint
   rules, config, docs. (Waits on ntfy removal + `OVERSEER_PRINCIPAL_ID` set on the servers.)

`/admin` and `/observe` coexist only for the window between slice 1 deploying and slice 3
deploying, the minimum for Overseer never to lose data.

## First slice

go-api serves `GET /observe/health` to Overseer's service-account token and 403s anyone else. The
owner curls it with Overseer's token and gets the same body `/admin/health` returns. The rest of
slice 1 fills in the other six routes behind the same gate.

## Must-holds

- Every `/observe/*` route answers 401 with no token, 403 for any principal other than
  `OVERSEER_PRINCIPAL_ID`, and 405/404 for any non-GET method.
- Every `/observe/*` response carries `Cache-Control: no-store` (streams: `no-cache`) and
  `X-Content-Type-Options: nosniff`.
- Each `/observe/*` JSON body decodes with Overseer's current mirrored DTO with no field lost
  (contract test over the same fixtures as the `/admin` equivalents).
- `/observe/events/stream` never carries a raw user id; `/observe/logs/stream` serves only
  records that passed the logging redactor.
- Every record on both streams carries its `corr_id` when the originating request had one.
- `/observe/metrics/live` serves bucket counts, never precomputed percentiles; route keys are
  route templates (never raw paths or ids), bounded by the reqmetrics cap.
- SSE streams end at token expiry, at shutdown and at 15 minutes; a 17th subscriber gets 429.
- `ACQUISITION_PAUSED=true` → the scheduler reports paused at startup; `DISABLED_JOBS=x` with an
  unknown `x` → startup fails with an error naming `x`.
- No slice adds or modifies an `/admin` route.
- After slice 3: the go-api router has no route under `/admin`.
- After slice 5: no Go package path contains `internal/admin`; config loads with no
  `OPERATOR_*` variable set.
- After slice 2: no Overseer goapi request path starts with `/admin`.

## Expected signals

- New: `GET /observe/{health,metrics/live,eval,acquisition,quality/discography}`,
  `GET /observe/{events,logs}/stream`.
- Gone after slice 3: every `/admin/*` route (404). Gone after slice 4: the rollup job's log
  lines.
- Unchanged: `/health`, all `/v1/*`, Overseer's own `/overseer/*` pages.
- Overseer: every bucket keeps collecting through each deploy; no bucket goes `source_down`
  for longer than one deploy restart; credential health stays ok.
- Telemetry: go-api request volume moves from `/admin/*` to `/observe/*` one-for-one after
  slice 2 ships; the p95 of the moved routes stays where `/admin` had it.

## Decisions

- Kill switches become startup settings (`ACQUISITION_PAUSED`, `DISABLED_JOBS`,
  `EVAL_METER_ENABLED`); runtime toggles removed. (owner: yes)
- Metrics-history rollup job, route, store and table deleted; Overseer owns history. (owner: yes)
- One principal `OVERSEER_PRINCIPAL_ID`; `OPERATOR_USER_ID` / `OPERATOR_READONLY_USER_ID`
  deleted; owner sets the new variable on the servers at ship; go-api reads both names until the
  final slice. (owner: yes)
- Default: module `internal/observe`, path prefix `/observe`.
- Default: ntfy/alert code is left to the other chat; slice 5 waits for it.
- The older local draft `docs/drafts/admin-removal/` is superseded by this plan.
