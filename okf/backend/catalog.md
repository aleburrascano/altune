---
type: Bounded Context
title: Catalog
description: The Track and Playlist aggregate context — a user's owned music metadata, dedup, playlist ordering, and audio file storage/streaming.
resource: services/go-api/internal/catalog/
tags: [bounded-context, hexagonal, go-api, domain-model, track, playlist, aggregate]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

Catalog is the identity-and-metadata bounded context for a user's saved music. Its two aggregate roots are `Track` (`domain/track.go`) and `Playlist` (`domain/playlist.go`), both identified by wrapped-UUID value objects (`TrackId`, `PlaylistId`) and owned by a `shared.UserId`.

`Track` carries title/artist/album plus optional metadata (year, genre, track_number, album_artist, isrc, duration) and an `AcquisitionStatus` enum (`AcquisitionPending`/`Ready`/`Failed`, zero-value = pending). The status/`AudioRef`/`FailureReason` invariant is enforced only through aggregate methods — `MarkReady(audioRef)`, `MarkFailed(reason)`, `RevertToPending()`, `IsStreamable()` — never by direct field mutation. `NewTrack` computes a `DedupKey` (`domain/dedup.go`: lowercase, NFKC-normalized, alphanumeric-stripped `title|artist|album`) used by the persistence adapter's `ON CONFLICT (user_id, dedup_key) DO NOTHING` upsert, so re-saving the same track by metadata is idempotent and returns the existing row (`TrackRepository.Add` returns `(stored, created, err)`).

`Playlist` holds an ordered `[]PlaylistTrack{TrackId, Position}` and enforces contiguous 0..N-1 positions and no-duplicate-track invariants through `AddTrack`/`RemoveTrack`/`Reorder` — never via raw slice manipulation. `ErrTrackAlreadyInPlaylist` and `ValidationError` are the domain's `CodedError`-carrying error types (HTTP status travels with the domain error via `HTTPStatus()`).

Ports (`ports/`): `TrackRepository`, `PlaylistRepository` (composed of `PlaylistStore` + `PlaylistTrackMutator`, split for ISP), `AudioStore` (`Exists`/`Store`/`Stream`/`Delete` plus an `AudioStream` = `io.ReadSeeker`+`io.Closer`), and `AcquisitionScheduler` (a one-method port the acquisition context implements, injected into catalog services so `AddTrackService` and `StreamTrackService` can trigger re-acquisition without importing acquisition).

Services (`service/`): `AddTrackService` (validates, dedups, publishes `track_added_to_library`, schedules acquisition for a freshly-created track), `ListTracksService`, `DeleteTrackService` (deletes the row then best-effort deletes the audio file, logging orphans rather than failing), `PlaylistService` (create/list/get/rename/delete + track membership; a documented AIDEV-NOTE flags a narrow non-transactional race in `AddTrack`), and `StreamTrackService` (serves audio, and on a storage-miss reconciles the track to `Failed` and reschedules acquisition via `recoverMissingAudio`).

Adapters: `adapters/handler/` (chi handlers: `TrackHandler`, `PlaylistHandler`, `StreamHandler`, all auth-gated via `auth.RequireUserID`); `adapters/persistence/` (`PgxTrackRepository`, `PgxPlaylistRepository` over pgx, sharing a `trackScanDest()` column-order helper so playlist-track joins reuse the exact track scan); `adapters/storage/` (`FilesystemAudioStore` with path-traversal-safe `safePath` and cross-filesystem-safe rename-or-copy, and `ObjectStorageAudioStore` over MinIO/S3).
