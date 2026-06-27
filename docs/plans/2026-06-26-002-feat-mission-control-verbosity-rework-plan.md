---
title: "feat: Mission Control — verbosity rework + UI redesign"
type: feat
status: active
date: 2026-06-26
origin: docs/brainstorms/2026-06-26-mission-control-requirements.md (follow-up)
mockups: .superpowers/brainstorm/3076-1782521839/content/ (ia-layout, tabbed-detail, all-screens, acq-eval-verbose)
note: Lean implementation plan. Formal spec deliberately skipped (operator decision). Design captured below.
---

# feat: Mission Control — verbosity rework + UI redesign

## Why

The shipped Mission Control console is too sparse to be useful: logs show only event
type (`request.start` / `request.complete`), the provider board shows only
`state · calls · ms`, and the acquisition/eval panels are unlabelled. The operator
cannot answer the actual questions — *what was searched, what did each provider
return, is the artwork/discography correct, why did acquisition fail, why did a query
regress*. The data largely exists in-process but is discarded or never surfaced.

## Design decisions (from brainstorm 2026-06-26)

- **Approach C — correlation backbone for discovery + in-place enrichment elsewhere.**
  Discovery gets a `corr_id`-keyed request store with payload capture and a
  drill-down; the other surfaces (acquisition, eval, logs, events, health) get verbose
  on their own terms (they aren't request-scoped).
- **Both inspection modes:** passive capture of real requests **and** an on-demand
  re-run inspector. Passive capture includes the **full stage waterfall**
  (merge → rank → final), folded in via a nil-safe optional recorder.
- **Full operator visibility:** the console may hold real users' queries + results in
  memory for the sole operator behind the auth gate. Relaxes the prior
  type+timestamp-only redaction on the event surface.
- **Multi-key pool / load balancer: OUT OF SCOPE.** Re-runs share live keys for now.
  Provider telemetry is designed so it can later break down per-key without rework. A
  per-provider rate-limit/quota signal is shown so the operator can *see* a provider
  approaching its limit. The pool + balancer is a separate future feature.
- **UI: tabbed console (shell A).** Glance **Overview** tab + one deep tab per surface
  (Discovery, Acquisition, Eval, Logs, Health). Master–detail drill-downs on Discovery,
  Acquisition, and Eval. See mockups (path in frontmatter).

## Constraints (unchanged from the shipped console)

- All telemetry in-memory + bounded (4 GB OCI box); resets on restart.
- Operator-gated `/admin`; never widen `/v1`, weaken `auth.Middleware`, or widen CORS.
- New recording transport must NOT regress the hot path: bounded bodies, bounded
  request ring, total-bytes ceiling, recording failures degrade silently.
- Domain imports nothing from adapters/framework; ports defined where consumed.

## Implementation slices

Backbone first (highest-leverage, riskiest), then per-surface backend, then the UI.
Each slice is independently verifiable.

### Phase A — discovery backbone

- **S1. Production-safe recording transport.** A bounded `RoundTripper` (cap body size,
  cap retained exchanges, total-bytes ceiling) that attaches each exchange to the
  request's `corr_id` (read from context). Distinct from `httptrace.Recorder` (which is
  unbounded/CLI-only). Wraps the shared outbound transport.
  - Verify: unit tests — body cap truncates, ring evicts oldest, total ceiling holds,
    `-race`; a benchmark guards hot-path overhead.
- **S2. Discovery request store + endpoints.** `corr_id`-keyed bounded ring holding
  `{query, user, providers[], exchanges[], stages{merged,ranked,final}, logs[], timings}`.
  Discovery service pushes a stage snapshot via a **nil-safe optional recorder** (eval/
  diversity callers pass nil — no pollution, mirrors the provider-health pattern).
  Endpoints: `GET /admin/requests`, `GET /admin/requests/{corr_id}`.
  - Verify: a real search populates the store; eval search records nothing; payloads +
    stages retrievable by corr_id.
- **S3. On-demand re-run inspector.** `POST /admin/rerun {query, kinds, providers}` runs
  the exported `Merge/Rank/EnforceDiversity/CollapseArtistDuplicates` core through a
  recording client (mirrors `cmd/discoverytrace`), returns the full
  raw→mapped→merged→ranked→final waterfall. Operator-gated. Honest notes: bypasses
  breakers, shares live keys, spends quota.
  - Verify: a re-run returns all five stages; gated to operator; no prod-path mutation.

### Phase B — per-surface verbosity (backend)

- **S4. Logs capture at DEBUG + full attrs.** Ring captures at DEBUG regardless of stdout
  level (so rich provider/breaker lines reach the panel). Snapshot/stream already carry
  all attrs — the gap is the capture level + the UI render (S10).
  - Verify: a DEBUG line emitted under stdout=INFO still reaches the ring.
- **S5. Acquisition job lifecycle.** Extend the scheduler status: per-job
  `{track, artist, album, source resolution, pipeline stage, progress, timings, corr_id}`
  + terminal outcomes with reasons, via the `AcquisitionStatusReader` interface.
  Endpoints: list + `GET /admin/acquisition/{id}`.
  - Verify: a job exposes stage + source detail; failures carry reason; `-race`.
- **S6. Eval per-query detail.** Hold per-query `{score, baseline, delta, verdict}` +
  the ranked list with position deltas + a cause hint. Endpoint: list + per-query.
  - Verify: a below-baseline query reports regressed with deltas; disabled/no-data states.
- **S7. Events enrichment.** With full visibility, event rows carry subject context (not
  just type+time). Adjust the tap payload + handler.
  - Verify: an event surfaces its subject; rates intact.
- **S8. Health detail endpoint.** Per-dependency `{status, latency, last_checked, error,
  config_state}` + process stats (uptime, goroutines, heap) behind the operator gate.
  - Verify: Redis-down returns the real error + impact; topology stays off the public path.
- **S9. Provider board enrichment.** Add p95, err%, per-status mix, last-query, and the
  rate-limit/quota signal to the existing `FetchSuccessStore`-backed snapshot.
  - Verify: snapshot carries p95 + quota signal; eval traffic excluded.

### Phase C — UI

- **S10. Tabbed console rebuild.** Replace `internal/admin/ui/index.html` with the tabbed
  shell: Overview + Discovery/Acquisition/Eval/Logs/Health tabs, master–detail
  drill-downs, the re-run inspector, expandable log rows, `corr_id` cross-links. Wire
  each tab to its endpoints (snapshot + SSE). Vanilla JS, embedded, no build step.
  - Verify: agent-browser — each tab renders + updates live; non-operator denied.

## Out of scope / follow-up

- Multi-key pool + load balancer (separate feature spec).
- Durable history for any surface (in-memory only, as today).
