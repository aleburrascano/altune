---
type: Database Table
title: playlists and playlist_tracks
description: Storage for the catalog context's Playlist aggregate and its ordered PlaylistTrack membership.
resource: services/go-api/migrations/001_baseline.sql, services/go-api/internal/catalog/domain/playlist.go, services/go-api/internal/catalog/adapters/persistence/playlist_repo.go
tags: [database-table, catalog, aggregate-root, playlist]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

Two tables in `001_baseline.sql` back the `Playlist` aggregate (see [catalog](../backend/catalog.md)). `playlists`: `id UUID PK`, `user_id UUID NOT NULL`, `name TEXT NOT NULL`, `created_at`/`updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`. `playlist_tracks` is the membership join table: `playlist_id UUID NOT NULL REFERENCES playlists(id) ON DELETE CASCADE`, `track_id UUID NOT NULL REFERENCES tracks(id) ON DELETE CASCADE` (see [tracks-table](tracks-table.md)), `position INTEGER NOT NULL`, with composite `PRIMARY KEY (playlist_id, track_id)` (a track cannot appear twice).

The Go aggregate (`playlist.go`) models membership as `PlaylistTrack{TrackId, Position}` value objects held in `Playlist.Tracks []PlaylistTrack`; `TrackCount int` is a read-side projection, not a stored column. `NewPlaylist` validates the name is non-empty and ≤100 chars (`validatePlaylistName`). Aggregate methods enforce invariants at every mutation: `AddTrack` rejects duplicates via `ErrTrackAlreadyInPlaylist` (a `CodedError` carrying HTTP 409); `RemoveTrack` and `Reorder` renormalize `Position` to a contiguous `0..N-1` range in-memory.

`PgxPlaylistRepository` (`playlist_repo.go`) implements `ports.PlaylistRepository`. `ListForUser` computes `track_count` via a correlated subquery. `RemoveTrack` runs in a transaction: delete the row, then `UPDATE ... SET position = sub.new_pos FROM (SELECT track_id, ROW_NUMBER() OVER (ORDER BY position) - 1 ...)` to re-contiguate positions server-side. `ReorderTracks` batches per-track position updates via `pgx.Batch` inside a transaction. `GetPreviewArtwork` selects up to 4 distinct non-null `artwork_url`s ordered by earliest position, for playlist-cover collage rendering.
