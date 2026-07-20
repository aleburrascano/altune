---
type: Subsystem
title: Admin alerting
description: The in-process alert monitor — a 30s-ticker Fix→Log→Signal cascade over pluggable Conditions, paging the operator via ntfy on the Signal tier only.
resource: services/go-api/internal/admin/alert/
tags: [admin, alerting, monitor, ntfy, mission-control]
verified_commit: b1b3e3867ff5d3319beb9b3d361d8625cea3ec94
---

`Monitor` (`monitor.go`) runs a single goroutine on a `time.Ticker` (30s in production) evaluating a slice of `Condition`s — each just a `Key` plus an `Eval(ctx) *Alert` closure. Single-goroutine by design: incident-tracking state (`firing map[string]bool`) needs no locking. `Eval` returning non-nil means the condition is currently firing; the monitor dedups by `Key` so an ongoing incident pages once, logging `alert.recovered` when it clears.

**Severity is the Fix→Log→Signal cascade** (`SeverityFix` < `SeverityLog` < `SeveritySignal`): only `SeveritySignal` reaches `AlertNotifier.Notify`; `SeverityLog` is logged (`alert.condition_firing`) but not paged; `SeverityFix` conditions are assumed self-healing and aren't even logged as firing. `AlertNotifier` is defined in `alert/` — the consumer — per "interfaces belong to consumers," with two implementations: `NopNotifier` (no push channel configured; the monitor still runs and logs) and `NtfyNotifier` (`notifier.go`, plain HTTP POST to an ntfy topic URL, 15s client timeout, `Priority: urgent` header). The topic URL's unguessability is the *only* auth on the push channel — there is no separate credential.

**Privacy discipline is an invariant, not a convention**: `Alert.Message` MUST carry only operational state names — never connection strings, hostnames, query text, or user ids. The doc comment on `Alert` states this explicitly because a violation here would leak into a push notification, outside any auth boundary.

**Coverage gap, covered off-box**: the monitor cannot observe the box being fully down — it dies with the box it's monitoring. That gap is covered by the off-box GitHub Actions uptime check (see [ci-cd-pipeline](../../playbooks/ci-cd-pipeline.md)), which pushes its own ntfy alert on a failed health poll, independent of this monitor.

`Shutdown` cancels the loop context and waits (bounded by the caller's context) on a `done` channel — part of the app's ordered shutdown sequence (see [app-wiring](../app-wiring.md)).
