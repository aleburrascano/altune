---
date: 2026-06-26
topic: mission-control
---

# Mission Control — backend operator console

## Summary

A locked-down, fully self-built `/admin` operator console served by the Go backend, giving the operator complete real-time visibility into production — logs, service health, provider status, live events, search-quality, and the acquisition pipeline — from a browser, without SSHing into the OCI box. Everything ships in v1; nothing is deferred behind a phased rollout.

---

## Problem Frame

The altune backend is deployed to a single OCI ARM VM and is **live right now with real users testing it**. Today the only way the operator can see what production is doing is to SSH into the box and read raw stdout logs. There is no at-a-glance answer to the questions that matter while users are on the system: Is everything up? Which of the four discovery providers is failing or rate-limited? Are events flowing? Is search quality regressing? Are background downloads succeeding? When something goes wrong, the operator finds out from a user complaint, then has to shell in and grep — slow, reactive, and impossible from a phone.

The backend already *produces* most of the needed signal but throws it away or hides it: provider outcomes are computed per request as `ProviderStatus` but never aggregated; domain events flow through an in-process SSE bus but only to clients; `/health` pings dependencies but always reports OK; `cmd/discoveryeval` scores search quality but only offline; the background acquisition goroutines are invisible even when shelled in. The cost is operational blindness on a production system that paying-attention users are actively exercising.

---

## Actors

- A1. Operator: the solo developer/owner — the only human user of the console; needs to read production state and be alerted to problems.
- A2. Backend service (`services/go-api/`): produces the telemetry, hosts and serves the console.
- A3. External monitor: an off-box checker whose sole job is to notice when the whole server is down — the one thing A2 cannot self-report.

---

## Key Flows

- F1. Glance check
  - **Trigger:** Operator opens the console (routinely, or after a user mentions something feels off).
  - **Actors:** A1, A2
  - **Steps:** Console loads → health tiles, provider board, event-feed rates, eval meter, and acquisition panel render current state and push live updates.
  - **Outcome:** Operator answers "is prod healthy?" in seconds without SSH.
  - **Covered by:** R1, R3, R5, R6, R7, R8, R9, R10

- F2. Failure investigation
  - **Trigger:** Provider board shows Discogs `circuit_open` / discovery feels degraded.
  - **Actors:** A1, A2
  - **Steps:** Operator sees the failing provider and its recent rate → opens logs panel → filters/groups by correlation ID of an affected request → reads the provider calls that request made.
  - **Outcome:** Operator identifies the failing dependency and cause without shelling in.
  - **Covered by:** R4, R6, R10

- F3. Total-down detection
  - **Trigger:** The whole VM/process is down (console itself unreachable).
  - **Actors:** A3, A1
  - **Steps:** External monitor's check fails → operator is alerted out-of-band.
  - **Outcome:** Operator learns the box is fully down even though the in-box console can't tell them.
  - **Covered by:** R12, R13

---

## Requirements

**Console shell & access**
- R1. A single operator console is served by the backend at a dedicated route, viewable in a standard browser on laptop or phone. It is not a screen inside the Expo mobile app.
- R2. Access is restricted to the operator's own account through the backend's existing authentication; the console is not public and has no anonymous access.
- R3. All panels update in real time via push (reusing the existing SSE mechanism); the operator never has to manually refresh to see current state.

**Panels (all six in v1)**
- R4. Live logs: a live tail of recent backend log lines with at least log-level filtering, plus grouping by correlation ID so the operator can see all activity — including outbound provider calls — for a single request.
- R5. Service-health tiles: per-dependency up/down status for the API process, Postgres, and Redis, shown green/red at a glance.
- R6. Provider status board: for each discovery provider (MusicBrainz, Discogs, Last.fm, Deezer), the current outcome across `ok` / `timeout` / `error` / `rate_limited` / `circuit_open`, including circuit-breaker open/closed state.
- R7. Live event feed: domain/SSE events as they occur, with per-type rates over a rolling window.
- R8. Discovery-eval meter: the latest `discoveryeval` pass-rate, refreshed on a schedule and surfaced as a regression indicator relative to a baseline.
- R9. Acquisition-pipeline panel: telemetry for the background download jobs — in-flight count, succeeded/failed counts, and failure reasons.

**Telemetry**
- R10. Relevant panels show rolling-window telemetry — rates, counts, and latencies (e.g., per-provider error-rate, requests/min, p95 latency) — computed in-process with in-memory counters, with no external metrics store required for these numbers.

**Honesty change**
- R11. `/health` is changed so it can report a non-OK (red) status when a dependency (Postgres or Redis) is down. Today it always returns OK; the health tiles cannot be honest until this changes.

**External monitoring & alerting**
- R12. An external check, running off the box, detects total-server-down — the failure mode the in-box console structurally cannot observe.
- R13. The operator is alerted out-of-band (e.g., phone push) on key conditions — a dependency down, a circuit breaker stuck open, or an error/failure spike — without having to be watching the console.

**Deeper backend (lowest priority — see Key Decisions)**
- R14. A self-hosted metrics/tracing backend provides deeper historical metrics and trace detail, self-hosted to stay $0 and owned. It is the lowest-priority element of v1 and the first to cut if build cost runs high.

---

## Acceptance Examples

- AE1. **Covers R11, R5.** Given Redis is down, when the operator views the console, the Redis health tile shows red and `/health` reports a non-OK status.
- AE2. **Covers R6, R10.** Given Discogs has returned 5 consecutive failures and its breaker is open, when the operator views the provider board, Discogs shows `circuit_open` with its recent error-rate.
- AE3. **Covers R4.** Given a discovery request fanned out to four providers, when the operator filters the logs panel by that request's correlation ID, all four provider calls for that request are shown together.
- AE4. **Covers R8.** Given the latest scheduled `discoveryeval` run scored below the baseline, when the operator views the eval meter, it indicates a regression.
- AE5. **Covers R12, R13.** Given the entire VM is unreachable, when the external check fails, the operator receives an out-of-band alert even though the console itself is down.

---

## Success Criteria

- The operator can answer all six questions — is prod up, which provider is failing, are events flowing, is search regressing, are downloads working, what do the logs say — from a browser in seconds, without SSHing into the OCI box.
- The operator learns about a total-server-down event without depending on the box itself.
- `ce-plan` can decompose this into implementation slices without having to invent panel behavior, data sources, scope, or success criteria.

---

## Scope Boundaries

- Long-term/durable log archive, search, and retention — v1 covers live tail + a short rolling window, not a historical log store.
- Changing the SSE event bus's reliability semantics — Mission Control *reads* events; it does not make the bus durable or alter ADR-0012's drop-on-overflow behavior.
- Observability for the mobile client / end users — this console is backend-operator-facing only.
- Multi-admin accounts, roles, or RBAC beyond the single operator account.
- Any SaaS observability vendor (Grafana Cloud, Datadog, Sentry, hosted log drains) — everything is self-built or self-hosted.

---

## Key Decisions

- **Self-built, backend-served single console, $0, no SaaS.** Rationale: the operator wants ownership, and the data already exists in-process — a custom page is a smaller, more aligned lift than standing up and learning four ops tools.
- **Reuse existing primitives as panel data sources** (`ProviderStatus`, the SSE event bus, `/health`, `cmd/discoveryeval`, the acquisition goroutines). Rationale: avoids building a parallel instrumentation layer; the panels surface signal the system already computes.
- **Self-built telemetry via in-memory rolling counters, not a metrics backend, for the panel numbers.** Rationale: on a single-process backend, in-memory counters plus correlation-grouped logs deliver the operator's needs without a second system.
- **The metrics/tracing backend (R14) is included but lowest-priority and first-to-cut.** Rationale: the operator chose no deferrals; the agent's standing recommendation is that it is unnecessary on a single box and most in tension with "one self-built page." Recorded as a decision to revisit once planning makes its build cost concrete.
- **`/health` is made honest (R11) as a prerequisite** for trustworthy health tiles.
- **One external check (R12) is the sole accepted off-box dependency**, because total-down detection is structurally impossible from inside the box.

---

## Dependencies / Assumptions

- The existing authentication can restrict a route to the operator's own account.
- The existing SSE mechanism is per-user today (ADR-0012); an operator console that observes system-wide events/signals will need a way to subscribe across the system — to be worked out in planning.
- `cmd/discoveryeval` can be run on a schedule and emit a machine-readable score for the meter.
- The deployment can host one external check and one push channel at $0 (self-hostable or free tier) without adopting a SaaS observability vendor.

---

## Outstanding Questions

### Resolve Before Planning

- (none — the operator chose to proceed; the metrics-backend dissent is recorded as a Key Decision to revisit during planning, not a blocker.)

### Deferred to Planning

- [Affects R3, R7][Technical] The SSE bus is per-user (ADR-0012). How does the admin console subscribe to system-wide events and signals? Needs codebase exploration.
- [Affects R4][Technical][Needs research] Live log tailing without a log backend — in-memory ring buffer of recent slog records vs. tailing stdout — and how much history the panel holds.
- [Affects R12][Technical] What runs the external "is the box up" check at $0 (self-hosted from elsewhere vs. a free external pinger) — the one accepted "no third parties" exception.
- [Affects R13][Technical] Push channel for alerting (e.g., ntfy/Telegram/Discord webhook) and where the alert-evaluation logic lives.
- [Affects R14][User decision + Technical] Whether the self-hosted metrics/tracing backend stays in v1 or is cut once its build cost is concrete — revisit early in planning.
- [Affects R8][Technical] Scheduling mechanism and baseline source for the `discoveryeval` meter.
