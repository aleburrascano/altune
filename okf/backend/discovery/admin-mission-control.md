---
type: Subsystem
title: Discovery admin / Mission Control
description: The operator console under /admin — an unauthenticated shell plus operator-gated data endpoints for request tracing, live event/log streams, alerting, and provider health.
resource: services/go-api/internal/admin/
tags: [discovery, admin, mission-control, observability, sse, alerting, subsystem]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

Mission Control's `AdminHandler` (`handler/admin_handler.go`) splits into an unauthenticated `ServeIndex` (the console shell, holds no data) and `RegisterData`, which the composition root wraps in `auth.Middleware` (see [[auth]]) + `OperatorOnly` (`handler/operator_middleware.go`) — fail-closed: an empty `OPERATOR_USER_ID` denies every request, and the middleware re-checks for a verified user id itself so a wiring mistake still fails closed. Data endpoints cover health, logs (+ SSE stream), event rates (+ SSE stream), provider status, acquisition status, eval meters, and the request drill-down (`/requests`, `/requests/{corrID}`, plus rerun and ad-hoc test-search).

`streamSSE[T]` (`handler/sse.go`) is the one generic used by both live streams: encodes each channel value as a Server-Sent Event until client disconnect or channel close, requiring `http.Flusher` support through the middleware chain.

`requeststore.Store` (`requeststore/store.go`) is the in-memory, correlation-id-keyed drill-down: each `Record` accumulates the outbound provider `Exchange`s a bounded recording transport captured, plus (via the discovery service's recorder seam) query, providers, and final results, or a `DetailTrace` for artist-discography/top-tracks/related fetches. Bounded on three axes — `defaultMaxRequests` (100), per-body bytes (64KB), total bytes (96MB) — and resets on restart like every Mission Control surface.

`providerhealth.Store` (`providerhealth/store.go`) is a 5-minute rolling window of per-provider scatter-gather outcomes (status counts, avg/p95 latency, error rate, rate-limited count), recorded off the ranking path by the discovery handler after each search.

`alert.Monitor` (`alert/monitor.go`) runs a single-goroutine ticker over `Condition`s implementing the Fix→Log→Signal cascade — only `SeveritySignal` pages the operator via `AlertNotifier`; incidents dedup by `Key` (fire once, log recovery). It cannot observe the box being fully down (an off-box uptime check covers that gap — see [[ci-cd-pipeline]]). `handler.EventFeed` (`handler/event_feed.go`) is the system-wide single tap consumer, keeping per-type rolling rates and fanning redacted events to connected SSE subscribers.
