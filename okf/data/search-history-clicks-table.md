---
type: Database Table
title: discovery_search_history and discovery_search_clicks
description: Per-user search-history ring buffer and click-tracking tables backing the discovery context's SearchHistoryEntry and SearchClick surfaces.
resource: services/go-api/migrations/001_baseline.sql, services/go-api/internal/discovery/adapters/persistence/history_repo.go
tags: [database-table, discovery, search-history, telemetry]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

Two independent tables in `001_baseline.sql`. `discovery_search_history`: `id UUID PK`, `user_id UUID NOT NULL`, `query`/`query_norm TEXT NOT NULL`, `executed_at TIMESTAMPTZ NOT NULL DEFAULT now()`, `result_clicked_signature TEXT` (nullable). Indexed on `(user_id, executed_at DESC)` for recency reads. `discovery_search_clicks`: `id UUID PK`, `user_id UUID NOT NULL`, `query_norm`/`result_signature TEXT NOT NULL`, `position INTEGER NOT NULL`, `confidence TEXT NOT NULL`, `clicked_at TIMESTAMPTZ NOT NULL DEFAULT now()`, indexed on `(user_id, query_norm, result_signature, clicked_at DESC)` to support sliding-window click dedup.

These map to the `SearchHistoryEntry` and `SearchClick` domain aggregates per `docs/ubiquitous-language.md` (identity `SearchHistoryEntryId`/`SearchClickId`, both UUID). Only `history_repo.go` (`PgxSearchHistoryRepository`, implementing `ports.SearchHistoryRepository`) exists — it covers the history table only, not a separate click repo file.

Three operations: `Insert` appends one row per search. `TrimToN` implements the ring-buffer eviction — `DELETE ... WHERE user_id = $1 AND id NOT IN (SELECT id ... ORDER BY executed_at DESC, id DESC LIMIT $2)`, keeping only the newest N rows per user. `ListDistinctRecent` returns the most recent entry per distinct `query_norm` (an inner join against a `GROUP BY query_norm` subquery selecting `MAX(executed_at)`), so a user's history view shows each query once, most-recent-first, rather than every repeated search.
