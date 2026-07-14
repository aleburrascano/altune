---
type: Database Table
title: tracks
description: Storage for the catalog bounded context's Track aggregate — a user's saved audio recording plus its acquisition lifecycle.
resource: services/go-api/migrations/001_baseline.sql, services/go-api/internal/catalog/domain/track.go, services/go-api/internal/catalog/adapters/persistence/track_repo.go
tags: [database-table, catalog, aggregate-root, track]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

The `tracks` table (defined in `001_baseline.sql`) persists the catalog context's `Track` aggregate root (see [[catalog]]). Columns: `id UUID PK` (default `uuid_generate_v4()`), `user_id UUID NOT NULL` (owning user, no FK — cross-context reference by id per `shared.UserId`), `title`/`artist TEXT NOT NULL`, `album TEXT` (nullable, mapped to Go's `""` when absent), `duration_seconds DOUBLE PRECISION`, `added_at TIMESTAMPTZ NOT NULL DEFAULT now()`, `artwork_url TEXT`, `acquisition_status TEXT NOT NULL DEFAULT 'pending'`, `dedup_key TEXT NOT NULL` with `UNIQUE (user_id, dedup_key)`, plus metadata added later: `year`, `genre`, `track_number`, `album_artist`, `isrc`, `audio_ref`, `failure_reason`.

The Go domain type (`track.go`) wraps identity in `TrackId` (unexported `uuid.UUID` field) and models `acquisition_status` as the `AcquisitionStatus` enum (`AcquisitionPending`/`Ready`/`Failed`, zero-value `Pending`) via `String()`/`ParseAcquisitionStatus`. The aggregate enforces the `audio_ref ↔ status` invariant through methods (`MarkReady` requires a non-empty `audioRef`; `MarkFailed` requires `reason` and clears `AudioRef`; `RevertToPending` clears both) — never via direct field mutation.

`PgxTrackRepository` (`track_repo.go`) implements `ports.TrackRepository`. `Add` uses `ON CONFLICT (user_id, dedup_key) DO NOTHING RETURNING id`; a conflict falls back to `GetByDedupKey` so callers get the existing row without a race-prone read-then-write. `Delete` runs inside a transaction that first deletes `playlist_tracks` rows referencing the track, then the track itself. A shared `trackScanDest()` helper builds the canonical 17-column scan/`Track` construction reused by joins in `playlist_repo.go` (see [[playlists-table]]).
