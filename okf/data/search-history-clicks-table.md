---
type: Database Table
title: discovery_search_history and discovery_search_clicks
description: Per-user search-history ring buffer (live) and a vestigial click-tracking table (no Go reader or writer — click telemetry actually lands in discovery_events).
resource: services/go-api/migrations/001_baseline.sql, services/go-api/internal/discovery/adapters/persistence/history_repo.go
tags: [database-table, discovery, search-history, telemetry, vestigial]
verified_commit: e238cc3671d1719837686c667242c7d88fc376d2
---

Two independent tables in `001_baseline.sql` — one live, one dead.

`discovery_search_history` (live): `id UUID PK`, `user_id UUID NOT NULL`, `query`/`query_norm TEXT NOT NULL`, `executed_at TIMESTAMPTZ NOT NULL DEFAULT now()`, `result_clicked_signature TEXT` (nullable). Indexed on `(user_id, executed_at DESC)` for recency reads. It backs the `SearchHistoryEntry` domain aggregate (`docs/ubiquitous-language.md`) via `history_repo.go` (`PgxSearchHistoryRepository`, implementing `ports.SearchHistoryRepository`), which has four operations: `Insert` appends one row per search. `TrimToN` implements the ring-buffer eviction — `DELETE ... WHERE user_id = $1 AND id NOT IN (SELECT id ... ORDER BY executed_at DESC, id DESC LIMIT $2)`, keeping only the newest N rows per user. `DeleteAllForUser` deletes every history row for a user in one statement (the clear-history path). `ListDistinctRecent` returns the most recent entry per distinct `query_norm` (an inner join against a `GROUP BY query_norm` subquery selecting `MAX(executed_at)`), so a user's history view shows each query once, most-recent-first, rather than every repeated search.

`discovery_search_clicks` (**vestigial**): `id UUID PK`, `user_id UUID NOT NULL`, `query_norm`/`result_signature TEXT NOT NULL`, `position INTEGER NOT NULL`, `confidence TEXT NOT NULL`, `clicked_at TIMESTAMPTZ NOT NULL DEFAULT now()`, with a `(user_id, query_norm, result_signature, clicked_at DESC)` dedup index. Nothing in the Go codebase reads or writes it — the table and its `SearchClick` aggregate came from the retired Python API (the `docs/ubiquitous-language.md` entry still points at the deleted `services/api/.../search_click.py`), and the baseline migration carried the DDL over. **Live click telemetry does not go here**: a click is persisted as a `result_clicked` `InteractionEvent` row in `discovery_events` via `PgxEventStore` (`adapters/persistence/event_repo.go`; see [discovery-events](discovery-events-table.md) and [telemetry](../backend/discovery/telemetry.md)). A maintainer extending click tracking should work with `discovery_events`, not this table; dropping `discovery_search_clicks` in a future migration would lose nothing.
