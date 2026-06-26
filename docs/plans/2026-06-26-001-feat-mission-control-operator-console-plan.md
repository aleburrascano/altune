---
title: "feat: Mission Control — backend operator console"
type: feat
status: active
date: 2026-06-26
deepened: 2026-06-26
origin: docs/brainstorms/2026-06-26-mission-control-requirements.md
---

# feat: Mission Control — backend operator console

## Summary

Build a locked-down operator console served by the Go backend at `/admin` — a single self-contained, server-embedded page (vanilla JS + SSE, no build step) with six live panels — by adding thin in-memory telemetry seams on top of primitives the backend already produces (`ProviderStatus`, the SSE bus, `/health`, the discovery eval logic, the acquisition scheduler), plus an in-process alert monitor and one external box-down check. Sequenced **alerting-first** so the operator gets "tell me when prod breaks" within the first few units, before the six-panel page. No external metrics/tracing system.

---

## Problem Frame

The backend is live on a single OCI ARM VM with users testing it now, and the only way to see what production is doing is to SSH in and read stdout. There is no at-a-glance answer to "is it up, which provider is failing, are events flowing, is search regressing, are downloads working, what do the logs say." The data largely exists in-process but is discarded or hidden. (Full motivation: see origin.)

**Stated limitation (eyes-open):** all panel telemetry is in-memory, so it **resets to empty on every restart** — including the deploy/crash/OOM moments the operator most wants to inspect. Post-crash root-cause must come from persisted stdout (journald/file capture on the VM), not the console. The alerting-first sequencing and the off-box check (U9) exist partly to compensate: they fire *before/around* the restart rather than relying on surviving state.

---

## Requirements

- R1. Single backend-served operator console at a dedicated route, browser-viewable, not in the Expo app.
- R2. Operator-only access via existing auth, restricted to a configured account.
- R3. All panels update in real time via push (reuse SSE); no manual refresh.
- R4. Live logs panel: live tail of recent log lines, level filtering, correlation-ID grouping.
- R5. Service-health tiles: API / Postgres / Redis up-down at a glance.
- R6. Provider status board: per-provider current state across `ok/timeout/error/rate_limited/circuit_open` + breaker state.
- R7. Live event feed with per-type rolling rates.
- R8. Discovery-eval pass-rate meter, refreshed on a schedule, shown vs. baseline.
- R9. Acquisition-pipeline panel: in-flight, succeeded/failed, failure reasons.
- R10. Rolling-window telemetry (rates, counts, latencies) on relevant panels, in-memory, no external metrics store.
- R11. `/health` (or a readiness sibling) can report non-OK when a dependency is down.
- R12. External off-box check detects total-server-down.
- R13. Out-of-band alerting on key conditions without watching the console.
- R14. *(Deferred — see Scope Boundaries.)* Self-hosted metrics/tracing backend.

**Origin actors:** A1 (operator), A2 (backend service), A3 (external monitor)
**Origin flows:** F1 (glance check), F2 (failure investigation), F3 (total-down detection)
**Origin acceptance examples:** AE1 (R11,R5), AE2 (R6,R10), AE3 (R4), AE4 (R8), AE5 (R12,R13)
**Plan-added acceptance examples:** AE6 (R3,R7), AE7 (R1,R2)

---

## Scope Boundaries

- No SaaS observability vendors (Grafana Cloud, Datadog, Sentry, hosted log drains).
- No durable log archive/search — live tail + bounded in-memory window only.
- No durable acquisition job history — in-memory status only in v1.
- No change to the SSE bus's reliability semantics or ADR-0012 drop-on-overflow behavior; the system-wide tap is read-only and lossy by design.
- No multi-admin accounts, roles, or RBAC beyond the single operator.
- No observability for the mobile client / end users — backend-operator-facing only.
- The system-wide event tap exposes **only event type + timestamp** (no payloads, no `user_id`, no `query_norm`) — see Key Technical Decisions (privacy).

### Deferred to Follow-Up Work

- R14 self-hosted metrics/tracing backend: deferred to a separate iteration. In-memory rolling counters + correlation-grouped logs cover the operator's needs on a single box; a standalone metrics/tracing system is a disproportionate second system to run and view. Confirmed cut during planning.

---

## Context & Research

### Relevant Code and Patterns

- `services/go-api/internal/app/app.go` — composition root; root middleware chain (`CorrelationID → Recoverer → RequestLogger → MaxBodySize → CORS`); `auth.Middleware(verifier)` is applied **on the `/v1` group only**, not globally; `handleHealth` (app.go:329) already pings DB + Redis and returns per-dependency JSON but **always 200**; `/v1/events` SSE handler; `Run` shutdown drains `vocabRefresh` then `scheduler`; DI of pgx pool, redis, config, event bus.
- `services/go-api/internal/shared/events/bus.go` — `Bus` interface; `InProcessBus` per-user ring (100); per-user `Publish` builds the subscriber slice under `us.mu`, releases, then non-blocking `select … default` drop with a per-bus `dropped` counter; `getOrCreateUser` lazily inits per-user state. **No system-wide subscription today.**
- `services/go-api/internal/shared/logging/logging.go` — `Setup(cfg)` builds one `slog.Handler` (`prettyHandler` dev / `slog.NewJSONHandler` prod), `slog.SetDefault`, **returns void**; `prettyHandler` implements `slog.Handler` incl. `WithAttrs` (returns a new handler). No in-memory buffer.
- `services/go-api/internal/auth/middleware.go` — JWT (Supabase JWKS) → `UserId` in context; `UserIDFromContext` / `RequireUserID`. **No admin/allowlist concept.**
- `services/go-api/internal/discovery/domain/types.go` — `ProviderStatus` enum + `String()`; per-request `ProviderSearchResponse.Status` set in `service/search.go` `fanOut`.
- `services/go-api/internal/discovery/ports/ports_telemetry.go` — **existing** `FetchSuccessStore` (`Record(ctx, provider, success)` / `GetRate(ctx, provider)`) per-provider telemetry port — extend this, don't duplicate.
- `services/go-api/internal/discovery/service/eval/` — the **importable** eval logic (`RunHarness`, report types, baseline load/compare). `cmd/discoveryeval/main.go` is `package main` and **not importable** — do not import it.
- `services/go-api/internal/acquisition/service/scheduler.go` — `BackgroundAcquisitionScheduler` tracks in-flight jobs in a `sync.Map`; logs only; **no status surface**.
- `services/go-api/internal/shared/config/config.go` — `caarlos0/env` + `godotenv`; add fields with `env:` tags + `Has*` helpers.

### Institutional Learnings

- ADR-0012 — the event bus is observation-only and drops silently; do not assume delivery. The system-wide tap inherits this lossy posture.
- ADR-0007 — per-provider circuit breaker (5 fails → open 30s) + bulkhead + 1500/2000ms timeout, no retries v1; `ProviderStatus` is the partial-failure surface.
- `docs/patterns/data-consistency.md` — Fix→Log→Signal cascade as the alert severity model; structured fields `(entity_id, reason, user_id)`.
- `.claude/rules/...` — define interfaces where consumed; no premature ports (2+ consumers / testability); domain imports nothing from adapters/framework.

### External References

- None — the build reuses local primitives and adds no new external technology.

---

## Key Technical Decisions

- **UI is a single server-embedded static page at `/admin`** (Go `embed`, vanilla JS + SSE), opened **same-origin at the backend's own URL** (so no CORS change is needed; CORS must **not** be widened — never `*` — for `/admin`).
- **Operator gating is a two-layer group middleware stack:** the `/admin` route group applies `auth.Middleware` **first**, then an operator middleware that calls `RequireUserID` (401 if absent), explicitly denies when `OPERATOR_USER_ID` is empty/unset (fail-closed, before any comparison), then checks the JWT `UserId` equals `OPERATOR_USER_ID`. `/admin` becomes the highest-value target on the box — single-credential blast radius is accepted for a solo operator but noted.
- **All panel telemetry is in-memory** (ring buffers + rolling counters), no metrics store; bounded for the 4GB box; resets on restart (see Problem Frame limitation).
- **The live event feed adds a read-only, bus-level (global) system-wide tap** — a single buffered channel on `InProcessBus`, populated **after** `us.mu.Unlock()`, with its **own** `tapDropped` atomic counter and a non-blocking `select … default` send; **exactly one consumer** (the admin handler), enforced. The admin handler depends on a small consumer-defined interface, **not** a change to the `Bus` interface.
- **Privacy:** the system-wide tap carries **only `(event_type, occurred_at)`** — never payloads, `user_id`, or `query_norm`. Per-type rolling rates are computed from type alone. (Family/friends deployment — one user must not see another's live searches.)
- **Provider-health metrics extend the existing `FetchSuccessStore` port** (add per-status-bucket counts + current breaker state) rather than a new parallel port; recorded from the scatter-gather where `ProviderStatus` is already determined, via a nil-safe optional recorder so eval/diversity callers of `BuildSearchService` don't pollute the live board.
- **Acquisition status is read through a narrow `AcquisitionStatusReader` interface** the scheduler implements and the admin handler consumes — no direct `admin → acquisition/service` struct coupling.
- **Alerting:** an in-process ops-monitor goroutine + an `AlertNotifier` interface **defined inline where consumed** (in `alert/monitor.go`); ntfy concrete adapter + no-op when unconfigured. **No `admin/ports/` package** (single consumer). Alert messages contain **only operational state names** (e.g. `dependency=postgres state=down`) — never connection strings, hostnames, or user-ids. ntfy topic is a **non-guessable random string** stored as a secret.
- **Eval meter (U7) is OFF by default** behind an env flag, uses a **dedicated HTTP client that bypasses the shared per-provider breakers** (so eval failures can't trip the breakers live users depend on), runs a **small fixed smoke query set** (not the full corpus), reuses the existing DB pool, and has a **skip-if-already-running** guard. Driven via the importable `internal/discovery/service/eval` logic, never by importing `cmd/discoveryeval` (package main).
- **Total-down is a scheduled GitHub Actions check** hitting a bare public liveness endpoint; the detailed per-dependency status stays behind the operator-gated `/admin` tile, and public `/health` is reduced to a bare `200`/`503` to avoid leaking dependency topology.

---

## Open Questions

### Resolved During Planning

- SSE per-user → system-wide (R3,R7): bus-level global tap, post-unlock, own drop counter, single consumer, payload-redacted (see Key Decisions).
- Log tailing (R4): in-memory ring-buffer `slog.Handler` teeing the existing handler; `Setup` returns the shared ring (pointer) so it survives `WithAttrs`/`WithGroup` derivation and is threaded to the admin handler; bounded recent history (default ~1–2k records, configurable); grouping by correlation-id in the page.
- External check (R12): scheduled GitHub Actions workflow curling the public liveness endpoint; alerts on failure; env-scoped secrets.
- Push channel + evaluator (R13): in-process ops-monitor goroutine; `AlertNotifier` defined inline; ntfy adapter.
- Metrics/tracing backend (R14): cut from v1.
- Eval scheduling/baseline (R8): off-by-default in-process ticker driving `internal/discovery/service/eval` with a dedicated breaker-bypassing client + smoke set; baseline from the existing eval baselines.

### Deferred to Implementation

- Exact ring-buffer sizes / eval interval / smoke-set size / rolling-window durations — tune against real log volume and the **tightest provider quota minus live-traffic headroom** (show the arithmetic when set).
- Whether honest-health extends `/health` in place or adds a `/ready` sibling — decide in U2; U9's probe target follows that decision.
- Exact `baselines` source path for the eval compare — confirm during U7.

---

## High-Level Technical Design

> *This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.*

```
Browser (/admin page, operator JWT, same-origin)
        │  embedded page + SSE streams + JSON snapshots
        ▼
[auth.Middleware → operator gate]  ── /admin route group ──┐
        ├── logs        ← shared in-mem ring slog handler (tees stdout)
        ├── health      ← existing per-dep check (now drives non-200 readiness)
        ├── providers   ← extended FetchSuccessStore (status buckets + breaker)
        ├── events      ← bus-level global tap (type+time only, lossy, 1 consumer)
        ├── eval        ← latest smoke-eval score (OFF by default; breaker-bypass client)
        └── acquisition ← AcquisitionStatusReader (scheduler in-mem status)

In-process ops-monitor ──(Fix→Log→Signal; state-name-only msgs)──▶ AlertNotifier ─▶ ntfy
External: GitHub Actions cron ──curl public liveness──▶ alert on failure (off-box; total-down)
Both new goroutines (eval ticker, ops-monitor) registered in app.Run shutdown drain.
```

---

## Implementation Units

Sequenced alerting-first. Build order: **U1 → U2 → U9 → U8(dep-down)** (operator has alerts), then **U3, U4, U5, U6, U7** (panels; U8 gains breaker conditions after U5), then **U10** (the page). U-IDs are stable and not renumbered by the reordering.

### Phase 1 — Foundation & alerting-first (eyes on prod within these units)

- U1. **Operator-gated `/admin` route shell + config**

**Goal:** A locked-down, operator-only `/admin` route group serving an (initially empty) embedded page.

**Requirements:** R1, R2

**Dependencies:** None

**Files:**
- Modify: `services/go-api/internal/shared/config/config.go` (`OPERATOR_USER_ID` + ntfy/eval/alert fields + `Has*` helpers)
- Create: `services/go-api/internal/admin/handler/admin_handler.go` (route group, static page serve)
- Create: `services/go-api/internal/admin/handler/operator_middleware.go`
- Create: `services/go-api/internal/admin/ui/index.html` (+ embedded JS/CSS via `embed.FS`)
- Modify: `services/go-api/internal/app/app.go` (mount `/admin` group with `auth.Middleware` then operator gate; inject config)
- Test: `services/go-api/internal/admin/handler/operator_middleware_test.go`

**Approach:**
- New `internal/admin` context. `/admin` group applies `auth.Middleware` first, then the operator middleware (two-layer stack — not auth at a different scope). Operator middleware: `RequireUserID` → 401 if absent; deny if `OPERATOR_USER_ID == ""`; then compare. Page from `embed.FS`, no build step.

**Patterns to follow:** `internal/auth/middleware.go`; `Mount`/`Route` wiring in `app.go`; functional-options config.

**Test scenarios:**
- Covers AE7. Happy path: authenticated request whose user id matches `OPERATOR_USER_ID` reaches the handler.
- Error path: authenticated non-operator id → 403.
- Error path: unauthenticated request → 401 (auth layer runs first even if group wiring is wrong).
- Edge case: `OPERATOR_USER_ID` unset/empty → denied (fail-closed), even for a request whose JWT yields a non-empty id.
- Edge case: empty-string JWT subject with unset config → denied (no zero-value match).

**Verification:** `/admin` serves only to the operator account; everyone else denied; unset config fails closed.

- U2. **Honest health (readiness can report red) + health-tile data + bare public liveness**

**Goal:** Readiness returns non-OK when a dependency is down (feeds tiles); public `/health` reduced to a topology-free liveness signal.

**Requirements:** R5, R11

**Dependencies:** U1

**Files:**
- Modify: `services/go-api/internal/app/app.go` (liveness vs readiness; per-dep status served via the operator-gated tile endpoint, not publicly)
- Test: health handler test

**Approach:**
- Public endpoint becomes a bare `200`/`503` liveness (process up / critical dep down) — no `{db,redis}` topology in the public body. The detailed per-dependency status (already computed in `handleHealth`) moves behind the `/admin` tile endpoint. Decide extend-`/health` vs add `/ready` here; U9 probes whatever this chooses.

**Patterns to follow:** existing `handleHealth` per-dep checks (reuse the `not_configured` vs `down` distinction for the tile).

**Test scenarios:**
- Covers AE1. Happy path: all deps up → readiness OK + each tile green.
- Error path: Redis down → readiness non-OK + Redis tile red, DB tile green.
- Error path: Postgres down → readiness non-OK + DB tile red.
- Edge case: public liveness body carries no per-dependency topology.

**Verification:** readiness flips non-OK + the matching tile reds when a dep is actually down; public endpoint leaks no topology.

- U9. **External box-down check (GitHub Actions)**

**Goal:** Detect total-server-down from off the box and alert.

**Requirements:** R12

**Dependencies:** U2 (liveness endpoint), U8 (alert channel, optional)

**Files:**
- Create: `.github/workflows/uptime-check.yml` (scheduled curl of the public liveness URL; alert on failure)

**Approach:**
- Scheduled workflow curls the public liveness URL (the endpoint U2 settles on); on non-OK/unreachable, notifies (reuse the ntfy topic or workflow-native notify). Off-box by construction → catches full-down the in-process monitor cannot. **Env-scoped** GitHub secrets; the only stored value is the ntfy topic/URL (health URL is public).

**Patterns to follow:** existing `.github/workflows/` scheduling/secrets conventions.

**Test scenarios:**
- `Test expectation: none — CI workflow config.` Validate via `workflow_dispatch` against a reachable and an unreachable endpoint; alert fires only on failure.

**Verification:** box reachable → silent pass; simulated unreachable → alert.

- U8. **In-process alert monitor + AlertNotifier (ntfy)**

**Goal:** Page the operator out-of-band on key conditions. Lands with the dependency-down condition first (needs only U2); gains breaker/provider conditions after U5.

**Requirements:** R13

**Dependencies:** U2 (dep-down condition); U5 (breaker/provider conditions — added when U5 lands)

**Files:**
- Create: `services/go-api/internal/admin/alert/monitor.go` (evaluator goroutine **+ the `AlertNotifier` interface defined here, where consumed**)
- Create: `services/go-api/internal/admin/alert/ntfy_notifier.go` (concrete + no-op)
- Modify: `services/go-api/internal/app/app.go` (wire monitor + notifier **and register its shutdown in `Run`'s drain**)
- Test: `services/go-api/internal/admin/alert/monitor_test.go`

**Approach:**
- Periodic evaluator over in-memory telemetry: dependency down (v1), then breaker stuck open / failure-rate spike (post-U5). Fix→Log→Signal — only Signal-tier pushes. Debounce: once-per-incident, not once-per-tick. Messages carry only state names. No-op notifier when unconfigured (mirror Redis-absent pattern). No `admin/ports/` package — single consumer.

**Patterns to follow:** `data-consistency.md` Fix→Log→Signal; Null-Object no-op; scheduler goroutine + context-cancel drain.

**Test scenarios:**
- Covers AE5. Happy path: a Signal condition (dep down) triggers exactly one notification.
- Edge case: condition persisting across ticks doesn't spam (once-per-incident).
- Error path: notifier failure is logged, monitor survives.
- Edge case: unconfigured notifier → no-op, monitor still runs.
- Edge case: message body contains no connection strings / hostnames / user-ids.

**Verification:** a simulated Signal condition produces one push; transient/Log-tier conditions don't page; unconfigured → no-op; shutdown cancels the goroutine.

### Phase 2 — Panel data sources (independent; any order after U1)

- U3. **In-memory ring-buffer log handler + logs panel feed**

**Goal:** Tail recent logs and stream new ones, grouped by correlation id, no log backend.

**Requirements:** R4

**Dependencies:** U1

**Files:**
- Create: `services/go-api/internal/shared/logging/ringbuffer_handler.go` (`slog.Handler` teeing existing handler + bounded ring)
- Modify: `services/go-api/internal/shared/logging/logging.go` (`Setup` composes the tee **and returns the shared `*RingBuffer`**; update the `cmd/discoveryeval` call site for the signature change)
- Modify: `services/go-api/internal/app/app.go` (thread the ring into the admin logs handler)
- Create: `services/go-api/internal/admin/handler/logs_handler.go` (snapshot + SSE stream, reusing the `sse_handler` `ctx.Done()` + `defer cancel()` shape)
- Test: `services/go-api/internal/shared/logging/ringbuffer_handler_test.go`

**Approach:**
- Wrapper holds a **pointer to one shared ring** that survives `WithAttrs`/`WithGroup` derivation (so per-request child loggers tee to the same ring). Forward to stdout, then append a **pre-copied** captured record (level, time, message, correlation-id + needed attrs) — **never retain the `slog.Record`** (its `Attrs` is unsafe post-`Handle`) — under a **dedicated short-held mutex, not the formatting lock**. Bounded preallocated ring.
- **Data sensitivity:** the ring retains whatever is logged (incl. `user_id`) in memory and serves it at `/admin`; access to the logs panel = access to retained user-ids. Acceptable behind operator gate; noted.

**Patterns to follow:** existing `prettyHandler` `slog.Handler`; `sse_handler.go` streaming shape.

**Test scenarios:**
- Covers AE3. Happy path: records sharing a correlation id are retrievable together for grouping.
- Happy path: forwarding intact — wrapping doesn't drop stdout output; `WithAttrs`-derived loggers still reach the shared ring.
- Edge case: ring at capacity evicts oldest, retains newest N.
- Edge case: level filter returns only records at/above the requested level.
- Concurrency: concurrent writes don't race (`-race`); a throughput benchmark before/after the tee guards against a hot-path contention regression.

**Verification:** the panel shows a live tail, filters by level, groups by correlation id; stdout logging and throughput are unaffected.

- U4. **System-wide event tap (redacted) + live event feed with rolling rates**

**Goal:** An operator firehose of event **types** with per-type rolling rates — no payloads.

**Requirements:** R7, R3, R10

**Dependencies:** U1

**Files:**
- Modify: `services/go-api/internal/shared/events/bus.go` (bus-level global tap: single buffered channel + own mutex + `tapDropped atomic.Uint64`; non-blocking send **after** `us.mu.Unlock()`; `SubscribeAll` on the **concrete** `*InProcessBus`, single-consumer-enforced)
- Create: `services/go-api/internal/admin/handler/events_handler.go` (consumes a tiny consumer-defined `interface{ SubscribeAll() … }`; SSE stream + rolling per-type rates)
- Test: `services/go-api/internal/shared/events/bus_test.go`; integration in `services/go-api/internal/shared/events/bus_integration_test.go`

**Approach:**
- Global tap distinct from `userState.subscribers`, fed at `Publish` after the per-user ring write — so it captures events for **never-before-seen users** too. Tap event carries **only type + timestamp**. The `Bus` interface is untouched; the admin handler depends on a narrow consumer-defined interface satisfied by the concrete bus.

**Patterns to follow:** existing per-user non-blocking-drop mechanics (as a *template* for the send shape, not the registration site).

**Test scenarios:**
- Covers AE6. Happy path: an event published for any user (including a never-seen user id) appears on the tap as type+timestamp only.
- Happy path: per-type rate reflects recent publish volume over the window.
- Edge case: slow/absent admin consumer drops events without blocking publishers (assert non-blocking + `tapDropped` increments, separate from per-user `dropped`).
- Concurrency: concurrent multi-user publishes — tap never appears in a publisher's critical section (`-race`).
- Integration: a real publish site (acquisition or catalog) surfaces on the tap; payload fields are absent.

**Verification:** the feed shows event types from all users with live rates; publishers never block; no payloads/user-ids cross the tap.

- U5. **Provider-health buckets on the existing port + provider status board**

**Goal:** Aggregate per-provider `ProviderStatus` outcomes + breaker state for the board, extending the existing telemetry port.

**Requirements:** R6, R10

**Dependencies:** U1

**Files:**
- Modify: `services/go-api/internal/discovery/ports/ports_telemetry.go` (extend `FetchSuccessStore` with per-status-bucket record/read + breaker state — or add a narrow companion in the same file if semantics diverge)
- Create/Modify: in-memory rolling-counter adapter under `internal/discovery/adapters/cache/` implementing the extended port
- Modify: `services/go-api/internal/discovery/service/search.go` (`fanOut` records each outcome via a **nil-safe optional recorder**; record outside the hot lock / via atomic counters)
- Modify: `services/go-api/internal/app/app.go` (construct + inject the in-mem adapter; ensure `BuildSearchService` passes nil/no-op for eval & diversity callers so synthetic searches don't pollute the board)
- Create: `services/go-api/internal/admin/handler/providers_handler.go`
- Test: provider-health adapter test under `internal/discovery/adapters/cache/`

**Approach:**
- Reuse `FetchSuccessStore` rather than a parallel port. Record after the status is appended in `fanOut`. In-memory adapter holds per-provider rolling counts by status + current breaker state. Domain untouched.

**Patterns to follow:** existing `FetchSuccessStore`; `adapters/cache/` precedent; the optional-MB nil-safe injection pattern.

**Test scenarios:**
- Covers AE2. Happy path: repeated `circuit_open` surfaces as open with error-rate on the board.
- Happy path: mixed outcomes → correct per-status counts over the window.
- Edge case: window rolls — stale counts age out.
- Concurrency: concurrent records don't race (`-race`).
- Integration: a scatter-gather run records outcomes for all four providers; an eval/diversity call with nil recorder records nothing.

**Verification:** the board shows each provider's state, status mix, and breaker state; eval traffic never appears on it.

- U6. **Acquisition status surface (via reader port) + acquisition panel**

**Goal:** Expose the scheduler's job state for the panel without cross-context coupling.

**Requirements:** R9

**Dependencies:** U1

**Files:**
- Modify: `services/go-api/internal/acquisition/service/scheduler.go` (in-memory status: in-flight set + succeeded/failed counters + bounded recent-failures `(track_id, reason)`; implement a small status-reader interface)
- Create: reader interface (in `internal/acquisition/ports/` or inline where consumed) — `AcquisitionStatusReader`
- Create: `services/go-api/internal/admin/handler/acquisition_handler.go` (holds the interface, not the scheduler struct)
- Test: `services/go-api/internal/acquisition/service/scheduler_test.go`

**Approach:**
- Thread-safe in-memory status alongside the existing in-flight `sync.Map`; increment succeeded/failed on terminal outcomes; bounded recent failures with structured `(track_id, reason)`. No DB persistence v1. Admin handler depends on `AcquisitionStatusReader`, honoring the dependency rule.

**Patterns to follow:** existing in-flight `sync.Map` + `LoadOrStore` dedup guard; `data-consistency.md` failure fields.

**Test scenarios:**
- Happy path: scheduling shows in-flight; completion increments succeeded + clears in-flight.
- Error path: a failure increments failed and records `(track_id, reason)`.
- Edge case: dedup (already in-flight) doesn't double-count.
- Concurrency: concurrent schedule/complete don't race (`-race`).

**Verification:** the panel shows in-flight count, succeeded/failed totals, and recent failure reasons; admin imports the reader interface, not the service struct.

- U7. **Discovery-eval meter (off-by-default, breaker-bypassing smoke run)**

**Goal:** Optionally run a small eval on a timer, hold the latest pass-rate, expose vs. baseline — without perturbing live traffic.

**Requirements:** R8

**Dependencies:** U1

**Files:**
- Create: `services/go-api/internal/admin/eval_scheduler.go` (flat file, **not** a sub-package — ticker + latest result in memory)
- Modify (only if baseline-compare must be reused verbatim): extract baseline load/compare from `cmd/discoveryeval/main.go` into `internal/discovery/service/eval`; otherwise call the existing importable eval logic directly
- Create: `services/go-api/internal/admin/handler/eval_handler.go`
- Modify: `services/go-api/internal/app/app.go` (wire + **register shutdown in `Run`'s drain**)
- Test: `services/go-api/internal/admin/eval_scheduler_test.go`

**Approach:**
- **OFF by default** behind an env flag; long default interval. Drives `internal/discovery/service/eval` with a **dedicated HTTP client that bypasses the shared per-provider breakers**, a **small fixed smoke query set**, and the **existing DB pool** (do not let it open a second pool). **Skip-if-running** guard. Stores latest report + comparison vs. the existing baselines. The interval × smoke-set × providers must sit under the tightest provider quota minus live-traffic headroom — record the arithmetic when configured.

**Patterns to follow:** `internal/discovery/service/eval` report types; scheduler goroutine lifecycle (context-cancel drain).

**Test scenarios:**
- Covers AE4. Happy path: a run scoring below baseline → reported regression.
- Happy path: a run at/above baseline → passing with the score.
- Edge case: before the first run (and when disabled) → "no data yet" / "disabled", not a false zero or pass.
- Error path: a failed run → surfaced as stale/error, not a silent pass.
- Edge case: eval failures do not trip the live provider breakers (dedicated client).

**Verification:** when enabled, the meter shows latest pass-rate vs. baseline and flags regressions; disabled by default; live breakers untouched; shutdown cancels the ticker.

### Phase 3 — Console UI

- U10. **Mission Control page — six panels over SSE**

**Goal:** The single embedded page wiring all panels to their endpoints with live updates.

**Requirements:** R1, R3, R4, R5, R6, R7, R8, R9, R10

**Dependencies:** U1–U7 (panels), U2 (tiles)

**Files:**
- Modify: `services/go-api/internal/admin/ui/index.html` (+ embedded JS/CSS) — six panels, SSE subscriptions, snapshot loads
- Test: `Test expectation: none — static client page; verified via panel endpoints' tests + manual browser check.`

**Approach:**
- Vanilla JS: on load, fetch each panel's snapshot, then subscribe to its SSE stream. Health tiles green/red; provider board grid; logs tail with level filter + correlation-id grouping; event-type feed with rates; eval meter vs baseline (or "disabled/no data"); acquisition counters + recent failures. Opened same-origin at the backend URL.

**Patterns to follow:** `.claude/rules/frontend/ui-testing-workflow.md` (agent-browser verification); existing SSE consumption shape.

**Test scenarios:**
- `Test expectation: none — presentational.` Verify via agent-browser: each panel renders and updates live; non-operator is denied.

**Verification:** opening `/admin` as the operator shows all six panels updating live without manual refresh.

---

## System-Wide Impact

- **Interaction graph:** new `/admin` group + two-layer operator gate; a ring-buffer handler wraps the global slog handler (every log call site); a bus-level tap at every `Publish`; provider-outcome recording in `fanOut`; a status surface on the acquisition scheduler; two new background goroutines (eval ticker, alert monitor).
- **Error propagation:** all new telemetry is read-only/observational — recording failures never break the request path (provider recording, tap, log tee degrade silently); notifier failures are logged, not propagated.
- **State lifecycle risks:** the eval ticker and alert monitor must honor context cancellation and are **registered in `app.Run`'s shutdown drain** alongside `vocabRefresh`/`scheduler` (covered by a `goleak` test); ring buffers / rolling counters are bounded. All in-memory telemetry resets on restart (stated limitation) — rely on persisted stdout for post-crash root-cause.
- **API surface parity:** `/admin/*` is a new operator-only surface; it must not widen `/v1`, weaken `auth.Middleware`, or widen CORS (never `*`). Public `/health` is narrowed to topology-free liveness.
- **Integration coverage:** the event tap, provider recording, and acquisition status surface each need a test proving the real producer feeds the admin reader.
- **Unchanged invariants:** existing `/v1` routes, the per-user SSE bus semantics (ADR-0012 drop behavior), the `Bus` interface, `ProviderStatus` wire DTO, and the `discoveryeval` CLI all stay unchanged; new work only adds read-only seams alongside them.

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Eval ticker burns provider quota / trips live breakers / competes for resources | OFF by default; dedicated breaker-bypassing client; small smoke set; reuse pool; skip-if-running; documented quota arithmetic. |
| Log-ring tee adds hot-path lock contention | Dedicated short-held mutex off the format lock; pre-copied record; preallocated bounded ring; throughput benchmark gate (not just `-race`). |
| Log ring + tap retain/serve user data | Tap is type+timestamp only; logs panel behind operator gate; data-sensitivity noted. |
| In-memory telemetry vanishes on restart — when most needed | Alerting-first + off-box check fire around the restart; persisted stdout (journald) is the post-crash source; stated as an eyes-open v1 limit. |
| Operator-gate misconfiguration exposes prod data | Two-layer stack, `RequireUserID` first, empty-config fail-closed, sacred fail-closed test; `/admin` noted as highest-value target. |
| System-wide tap blocks publishers / misses new users | Bus-level global tap, post-unlock non-blocking send, own drop counter, single consumer; adversarial new-user + `-race` tests. |
| Telemetry seams add memory pressure on 4GB VM | All buffers/counters bounded + configurable; no durable history. |
| ntfy topic guessable / leaks detail | Non-guessable random topic as secret; messages carry only state names. |

---

## Documentation / Operational Notes

- New env vars (`OPERATOR_USER_ID`, eval enable/interval, ntfy topic/settings) → `.env.example` with docs; eval defaults OFF.
- Confirm the VM persists stdout to journald/file (post-crash root-cause depends on it); document in the deploy runbook.
- Strong `/compound-learning` + ADR candidate after landing: record the self-built-console decision and the R14 deferral (observability stance is currently "TBD via ADR" in `docs/architecture.md`).
- Document the ntfy topic and the env-scoped GitHub Actions secret(s) for the uptime check.

---

## Sources & References

- **Origin document:** [docs/brainstorms/2026-06-26-mission-control-requirements.md](docs/brainstorms/2026-06-26-mission-control-requirements.md)
- Ideation: [docs/ideation/2026-06-26-backend-observability-ideation.md](docs/ideation/2026-06-26-backend-observability-ideation.md)
- Related decisions: `docs/adr/0007-unified-music-search.md`, `docs/adr/0012-direct-call-acquisition-triggering.md`, `docs/patterns/data-consistency.md`
- Key code: `internal/app/app.go`, `internal/shared/events/bus.go`, `internal/shared/logging/logging.go`, `internal/auth/middleware.go`, `internal/discovery/domain/types.go`, `internal/discovery/ports/ports_telemetry.go`, `internal/discovery/service/eval/`, `internal/acquisition/service/scheduler.go`
