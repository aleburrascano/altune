---
date: 2026-06-26
topic: backend-observability
focus: observability platform for the backend
mode: repo-grounded
---

# Ideation: Observability for the altune Go backend

## Grounding Context (codebase)
Already exists: `slog` (JSON prod / text dev) at `internal/shared/logging`, `CorrelationID` middleware (8-char id), `RequestLogger`, `Recoverer` (panic+stack), `GET /health` (pings db+redis but **always returns 200**), an in-process SSE event bus (per-user, drops silently — ADR-0012), domain events, and `cmd/discoveryeval` (offline search-quality baselines).

Absent: metrics, distributed tracing, error reporting, dashboards, alerting, SLOs.

Constraints: ADR-0007 (`ProviderStatus` = ok/timeout/error/rate_limited/circuit_open on the wire; per-provider circuit breaker; **no retries v1**), ADR-0012 (SSE bus is observation-only, drops silently), `data-consistency.md` (reserved metric names + `entity_id/reason/user_id` field contract). Single ~4GB OCI ARM VM + Supabase Postgres + self-hosted Redis, solo-operated, ~10 users, heavy outbound scatter-gather.

## Refined direction (from user, 2026-06-26)
The user does **not** want the standard ops stack (Grafana / OTel / Sentry). The actual goal is a **self-built admin dashboard — "Mission Control"** — a locked-down `/admin` surface that answers operator questions without SSHing into the OCI box:

1. **Live logs, no SSH** — prod logs streamed to a web page.
2. **Service health at a glance** — green/red tiles for API, Postgres, Redis.
3. **Provider status board** — live error / rate-limit / timeout state per provider (MusicBrainz, Discogs, Last.fm, Deezer).
4. **Live event feed + percentages** — events as they arrive, with rates.
5. **Discovery-eval regression meter** — live eval pass-rate to catch search-quality regressions.

"Build it ourselves" is realistic because the data largely already exists: `ProviderStatus` feeds the status board, the SSE event bus feeds the live feed, `cmd/discoveryeval` feeds the regression meter, `/health` feeds the tiles.

Two additions flagged: (a) one **external uptime ping** is the only piece worth not self-building — an in-box dashboard can't detect its own box being down; (b) the dashboard must be **admin-auth-gated**, and `/health` must be allowed to report **red** (not always-200) for the tiles to be honest.

**Chosen for brainstorm:** the self-built Mission Control dashboard (synthesizes survivors #1, #3, #4, #5 below into one product surface).

## Ranked Ideas

### 1. Honest /health — readiness/liveness split + auth-gated state + pprof
**Description:** Split always-200 `/health` into liveness + a readiness probe that returns non-200 when Postgres/Redis is down; add auth-gated `/debug/state` and `net/http/pprof`.
**Warrant:** `direct:` `/health` pings deps but always returns 200, so nothing can detect a degraded box.
**Rationale:** Highest signal-per-line change; prerequisite for honest health tiles and any uptime monitoring.
**Downsides:** Always-200 may be deliberate; must decide what consumes a red signal; debug endpoints need auth.
**Confidence:** 92% · **Complexity:** Low · **Status:** Explored

### 3. ProviderStatus as the observability vocabulary — per-provider RED + breaker gauges
**Description:** Aggregate the existing `ProviderStatus` enum into rolling per-provider counters + breaker-state gauge; feeds the status board.
**Warrant:** `direct:` ADR-0007 already surfaces ProviderStatus on the wire; this aggregates an existing value.
**Rationale:** With no retries v1, timeout vs rate_limited vs circuit_open is the signal that decides operator action; same vocabulary across API, dashboard, glossary.
**Downsides:** `circuit_open` is a designed state — alert thresholds need care. Keep `entity_id/user_id` out of metric labels (cardinality).
**Confidence:** 90% · **Complexity:** Low-Medium · **Status:** Explored

### 4. Make the silent event bus loud — drop counter + produce/consume reconciliation
**Description:** Add `sse_events_dropped_total` at the drop edge + a produce-vs-consume reconciliation; powers honest live-feed rates.
**Warrant:** `direct:` ADR-0012 drops events silently by design.
**Rationale:** Makes the one invisible-by-design component auditable without making it reliable.
**Downsides:** Resist over-building into a durable queue.
**Confidence:** 80% · **Complexity:** Low · **Status:** Explored

### 5. Synthetic probes + discovery-eval-as-live-meter
**Description:** In-process prober exercises critical paths on a ticker; surface `cmd/discoveryeval` pass-rate live as the regression meter.
**Warrant:** `reasoned:` at ~10 users real traffic is too sparse to baseline; `direct:` discoveryeval already scores the real path.
**Rationale:** Continuous heartbeat + a live quality number; unifies "outage" and "quality regression" into one view.
**Downsides:** Probes consume real provider quota; fixtures drift as upstream catalogs change.
**Confidence:** 78% · **Complexity:** Medium · **Status:** Unexplored

### 2. Single-seam auto-instrumentation — otelhttp + otelslog + exemplars
**Description:** Wrap the shared outbound transport once for spans+metrics; bridge slog to carry trace ids.
**Warrant:** `external:` one wrap instruments every current/future provider; `direct:` per-provider bulkhead partitions metrics for free.
**Rationale:** Highest-leverage technical foundation — but heavier and more jargon than the user's self-built goal needs for v1. Revisit if Mission Control outgrows hand-rolled data.
**Downsides:** Context-propagation fragility; needs a metrics/trace backend (off-box vs on-box fork); more concepts than the user wants now.
**Confidence:** 88% · **Complexity:** Medium · **Status:** Unexplored

### 6. $0 phone-first alerting + daily digest
**Description:** In-process evaluator over self-reported state pushes to a free channel (ntfy/Telegram); Fix→Log→Signal as the severity ladder.
**Warrant:** `direct:` alerting absent; Fix→Log→Signal already a decided 3-tier taxonomy.
**Rationale:** Matches the solo-operator attention budget; keeps the stack genuinely $0.
**Downsides:** In-process evaluator dies with the box — pair with the external ping.
**Confidence:** 75% · **Complexity:** Low-Medium · **Status:** Unexplored

### 7. Acquisition pipeline telemetry + Andon stop-the-line
**Description:** Instrument the invisible yt-dlp background goroutines (start/progress/terminal states, in-flight counts) + an Andon threshold that pauses enqueues on repeated failure.
**Warrant:** `direct:` background acquisition is unobserved; `data-consistency.md` reserves `tracks_failed_total`.
**Rationale:** The only blind spot outside the request cycle, on the most resource-hungry work.
**Downsides:** Pause/resume control path needs careful shutdown semantics.
**Confidence:** 82% · **Complexity:** Medium · **Status:** Unexplored

## Rejection Summary

| Idea | Reason Rejected |
|---|---|
| Correlation-id "poor-man's trace" waterfall | Superseded by #2 |
| Black-box ring buffer / all-22 / 64GB capture | Brainstorm variant of #2; heavier |
| Library-as-SQL-dashboard | Covered by #3 + #7 |
| Triage acuity / R₀ contagion / degree-day normalization | Speculative for current scale |
| GlitchTip error reporting (standalone) | Folded into #6 |
| Observability-as-product (dashboards-first) | A method, not an idea — adopt while building |
| Reserved-metric-name registry (standalone) | Folded into #2/#3 as guardrail |
