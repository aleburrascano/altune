---
type: Bounded Context
title: Playback (server)
description: Server-side persistence of a user's playback Queue snapshot, enabling resume-on-reopen; the live queue itself is client-owned.
resource: services/go-api/internal/playback/
tags: [bounded-context, hexagonal, go-api, queue, memento, resume]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

Playback (server) is deliberately thin: per ADR-0010, the live `Queue` (advance/prev/shuffle/repeat) is owned entirely by the mobile client (`apps/mobile/src/shared/playback/queueStore.ts`, see [[shared-playback]]). This backend context persists only a snapshot of it — the server half of the Memento pattern used for resume-on-reopen.

**Domain** (`domain/queue_state.go`): `QueueState{UserId, TrackIds []string, CurrentIdx, PositionMs, Shuffled, RepeatMode, SourceId, UpdatedAt}`. `TrackIds` is deliberately `[]string`, not `[]catalog/domain.TrackId` — an AIDEV-DECISION documents that playback references catalog tracks by id across the context seam rather than coupling to catalog's identity type for a snapshot the server never reasons about domain-wise.

`RepeatMode` is a three-state enum (`RepeatOff` zero-value / `RepeatAll` / `RepeatOne`) with `String()`/`ParseRepeatMode`, mirroring the mobile `RepeatMode` union.

All construction funnels through the unexported `newQueueState`, the single invariant gate: `PositionMs >= 0`; an empty `TrackIds` normalizes `CurrentIdx` to 0; a non-empty one requires `CurrentIdx` in range; a nil `trackIds` is normalized to `[]string{}` so callers and JSON serialization never see nil. Three public constructors funnel through it — `NewQueueState` (fresh, stamps `UpdatedAt=now`), `RehydrateQueueState` (reconstitutes from storage, preserving stored `UpdatedAt` — so a corrupt row can't produce an invalid `QueueState`), and `EmptyQueueState(userId)` (the canonical "no queue" value, used whenever no row exists for a user).

**Port** (`ports/queue_state_repo.go`): `QueueStateRepository{Upsert, GetForUser}` — a 2-method port, one per operation, no read/write coupling.

**Service** (`service/queue_service.go`): `QueueService.Save` parses the wire `RepeatMode` string, constructs a `QueueState` (rejecting invalid input before any I/O), and upserts. `QueueService.Resume` returns the saved snapshot or `EmptyQueueState` — callers never receive nil, so the handler needs no nil-check.

**Adapters**: `adapters/handler/queue_handler.go` exposes `PUT /queue-state` (save) and `GET /queue-state` (resume) behind `auth.RequireUserID`, mapping wire DTOs (snake_case JSON) to/from the service input/output. `adapters/persistence/queue_state_repo.go` (`PgxQueueStateRepository`) is a single-row-per-user upsert (`ON CONFLICT (user_id) DO UPDATE`) against `playback_queue_state` (see [[playback-queue-state-table]]), storing `track_ids` as a native array column and `repeat_mode` as its string form, rehydrating via `domain.RehydrateQueueState` so the same invariant gate applies on read.
