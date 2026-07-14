---
type: Database Table
title: playback_queue_state
description: One-row-per-user server-persisted snapshot of the playback Queue, used only for resume-on-reopen.
resource: services/go-api/migrations/003_playback_queue_state.sql, services/go-api/internal/playback/domain/queue_state.go, services/go-api/internal/playback/adapters/persistence/queue_state_repo.go
tags: [database-table, playback, queue, snapshot, memento]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

`playback_queue_state` (migration `003_playback_queue_state.sql`) has `user_id UUID PRIMARY KEY` — exactly one row per user, no history. Columns: `track_ids TEXT[] NOT NULL DEFAULT '{}'`, `current_idx INTEGER NOT NULL DEFAULT 0`, `position_ms BIGINT NOT NULL DEFAULT 0`, `shuffled BOOLEAN NOT NULL DEFAULT FALSE`, `repeat_mode TEXT NOT NULL DEFAULT 'off'`, `source_id TEXT NOT NULL DEFAULT ''`, `updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`.

The Go domain type `QueueState` (`queue_state.go`, see [[playback]]) is deliberately anemic toward `catalog`: `TrackIds` is `[]string`, not a `TrackId` value object — an explicit `AIDEV-DECISION` comment states this is reference-by-id across the context seam, avoiding coupling playback to catalog identity for a snapshot the server never reasons about domain-wise. `RepeatMode` is a 3-value enum (`RepeatOff`/`RepeatAll`/`RepeatOne`, zero-value `Off`). A single private constructor `newQueueState` is "the single door every QueueState passes through": it validates `positionMs >= 0`, normalizes a nil `trackIds` to `[]string{}`, and forces `currentIdx` in-range (or 0 for an empty queue) — reused by both `NewQueueState` (fresh, stamps `UpdatedAt = now`) and `RehydrateQueueState` (from storage, preserving stored `UpdatedAt`). `EmptyQueueState` is the canonical "no queue" value.

`PgxQueueStateRepository` (`queue_state_repo.go`, implements `ports.QueueStateRepository`) has exactly two operations: `Upsert` (`INSERT ... ON CONFLICT (user_id) DO UPDATE SET ...`, full-row replace) and `GetForUser` (returns `nil, nil` on `pgx.ErrNoRows` — no row is not an error, just "no snapshot yet").
