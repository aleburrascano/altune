---
type: Index
title: Admin / Mission Control
description: The single-operator observability console under /admin — an unauthenticated shell plus operator-gated data endpoints for request tracing, live event/log streams, alerting, provider health, and operator-triggered re-runs.
tags: [index, admin, mission-control, observability, sse, alerting, operator]
verified_commit: b1b3e3867ff5d3319beb9b3d361d8625cea3ec94
---

Mission Control is **deliberately not hexagonal** — no domain/ports/service/adapters split; it's a flat set of observability packages (`handler/`, `alert/`, `evalmeter/`, `eventtap/`, `providerhealth/`, `requeststore/`, `ui/`) sized for the 4 GB production box, not for change-tolerance under a growing feature set. It's a single-operator console with one deployment target, not a bounded context with independent evolution pressure on its own domain model — the hexagonal split earns nothing here that a flat package layout doesn't already give. Interfaces exist only where they earn their keep (structure-audit F2, 2026-07-16): `AdminHandler` holds same-feature collaborators as concrete pointers (`*eventtap.Feed`, `*providerhealth.Store`, `*requeststore.Store`, `*evalmeter.Meter` — an interface there cut no import and reintroduced the typed-nil trap) and keeps consumer-defined interfaces (`ReRunner`, `SearchInspector`, `AcquisitionStatusReader`, `HealthProbe`, `MetricsHistoryReader`, `evalmeter.Runner`) exactly where the implementation lives in the composition root (see [app-wiring](../app-wiring.md)). Construction follows one rule (F5): `New(probe, logRing)` takes the two always-present collaborators; every nil-tolerant panel dependency arrives via a chained `WithX` setter and degrades its panel when absent. **Dependency direction**: admin imports `discovery/domain` (trace projections); discovery and acquisition never import admin — they feed it through consumer-defined seams (`providerHealthRecorder`/`searchTraceRecorder` in the discovery handler; the acquisition scheduler satisfies `AcquisitionStatusReader` structurally).

**Gating**: `AdminHandler` splits into an unauthenticated `ServeIndex` (the console shell, holds no data; `GET /admin/config` returns only public Supabase client values so the page can sign in) and `RegisterData`, which the composition root wraps in `auth.Middleware` (see [auth](../auth.md)) + `OperatorOnly` — fail-closed: an empty `OPERATOR_USER_ID` denies every data request, and the middleware re-checks for a verified user id itself so a wiring mistake still fails closed.

**Everything is in-memory and resets on restart**, with one deliberate exception: the discovery-owned `discovery_metrics` rollup (see [discovery-metrics-table](../../data/discovery-metrics-table.md)) is the only durable history, read via `GET /admin/metrics?metric=<name>&days=<n>`.

## Subsystems

- [console-surfaces](console-surfaces.md) — the `AdminHandler` transport layer, gating, and the event-tap/eval-meter/provider-health/embedded-UI panels behind it
- [request-tracing](request-tracing.md) — the correlation-id-keyed `requeststore` drill-down: bounded recording HTTP transport plus discovery-fed search/detail traces
- [alerting](alerting.md) — the in-process `alert.Monitor`: 30s-ticker Fix→Log→Signal cascade, ntfy on the Signal tier only

**Quota discipline**: the eval meter (`evalmeter.Meter`, `EVAL_METER_ENABLED`, off by default) and `POST /rerun` both hit real provider APIs on live keys; both are breaker-isolated by *instance separation* — the eval meter gets its own `BuildSearchService` stack with a nil event store (synthetic searches never pollute telemetry), and the re-runner builds providers directly with no Service so it bypasses breakers by construction. Since structure-audit F1 (2026-07-16) the re-run ranks through the exported `RankOptions`/`RankWith`/`Reshape` composition with the same flag-gated stages and behavioral snapshot production applies, so the waterfall cannot silently diverge from live ranking (see [app-wiring](../app-wiring.md)).
