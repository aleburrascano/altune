---
type: Database Table
title: discovery_metrics
description: Nightly rollup table giving Mission Control restart-surviving, week-over-week history of discovery quality metrics.
resource: services/go-api/migrations/007_discovery_metrics_rollup.sql, services/go-api/internal/discovery/adapters/persistence/metrics_rollup_repo.go
tags: [database-table, discovery, metrics, rollup, mission-control]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

`discovery_metrics` (`007_discovery_metrics_rollup.sql`): `id UUID PK`, `as_of DATE NOT NULL`, `metric TEXT NOT NULL`, `value DOUBLE PRECISION NOT NULL`, `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`, with `UNIQUE (as_of, metric)` making the rollup idempotent per day/metric. Indexed on `(metric, as_of DESC)` for week-over-week reads. The migration comment states `user_id` is deliberately absent — rows are pure aggregates, no per-user cardinality or privacy cost. This durably persists what the operator console's in-memory counters would otherwise lose on restart (see [admin](../backend/admin.md)).

`PgxMetricsRollup` (`metrics_rollup_repo.go`, implements `ports.MetricsRollupStore`) has two operations. `RollupDay` computes a UTC day's four metrics in one CTE over `discovery_events` (see [discovery-events-table](discovery-events-table.md)) — `searches` (count of `search_performed`), `zero_result_rate` (fraction flagged `zero_result` in payload), `ctr` (distinct `search_id`s with a `result_clicked` / `searches`), `tail_noise_top5_avg` (average of `payload->>'tail_noise_top5'` where present) — then upserts all four rows via `INSERT ... UNION ALL SELECT ... ON CONFLICT (as_of, metric) DO UPDATE SET value = EXCLUDED.value, created_at = now()`. Read-only over `discovery_events`; a 6-hourly job drives it per ubiquitous-language. `MetricsHistory` returns the last N daily values of one named metric, newest first, for the Mission Control trend chart.
