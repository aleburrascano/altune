# Catalog — Data Retention & Deletion Policy

This document records the retention/deletion story for catalog's personal data and
the explicit design decision about a bulk per-user erasure path. It is the written
companion to the playback module's erasure precedent
(`internal/playback/ports/queue_state_repo.go`, `DeleteForUser`).

## What personal data catalog holds

- **Track rows** — a user's library metadata (title, artist, album, featured
  artists, acquisition status, audio reference).
- **Playlist rows** and their track membership.
- **Audio objects** — the actual audio files in the audio store (S3 / filesystem),
  addressed by the `audio_ref` carried on a ready track.

Every catalog row is user-scoped (`shared.UserId`), so all catalog PII is
partitioned by user.

## Deletion path (present tense)

Deletion is **per item**, driven by the caller:

- `TrackRepository.Delete(ctx, id, userId)` removes one track row and returns its
  `audioRef`. Playlist membership is removed by the database cascade (see #415), so
  deleting a track needs no separate membership cleanup.
- `PlaylistRepository.Delete(ctx, id, userId)` removes one playlist.
- After the track row is deleted, `DeleteTrackService.Execute` deletes the audio
  object through `AudioStore.Delete`.

### Partial-deletion handling (audio orphans)

A track delete touches two stores: the database and the audio store. The database
delete can succeed while the audio-store delete fails, leaving a user's personal
audio file behind as an **orphan**.

`DeleteTrackService.Execute` does **not** report this as a successful deletion. On
an audio-store delete failure it:

1. increments the `AudioStoreMetrics.OrphanedDelete()` counter (dashboard/alert
   surface), and
2. emits a marked log line (`event=catalog.orphaned_audio`) carrying `track_id`
   and `audio_ref`, so orphans are discoverable and reconcilable by querying that
   event rather than being lost in noise, and
3. returns `ErrAudioOrphaned` (HTTP 500, code `catalog.audio_orphaned`) so the
   caller learns the deletion was partial instead of being told it fully
   succeeded.

Reconciliation: an operator (or a future sweep job) lists orphans from the
`catalog.orphaned_audio` log event / `OrphanedDelete` counter and retries the
audio-store delete for the recorded `audio_ref`s.

## Decision: no dedicated bulk per-user erasure method (for now)

Playback exposes `QueueStateRepository.DeleteForUser` because its queue state is a
single per-user blob with free-text PII that has no other deletion trigger.
Catalog is deliberately **not** given an equivalent bulk `DeleteAllForUser` method
at this time:

- Catalog PII is already fully erasable through the existing user-scoped per-item
  `Delete` operations plus the database cascade; there is no PII that only a bulk
  method could reach.
- Account-deletion orchestration across modules is explicitly **out of scope** (see
  ticket #430). A bulk catalog erasure method only earns its keep once that
  orchestration exists and needs a single call to wipe a user's catalog.
- Adding the method now would mean a speculative, unexercised port + adapter
  implementation with no caller — carrying capability with nothing driving it.

When account-deletion orchestration is built, the chosen shape is a
`DeleteAllForUser(ctx, userId)` method on the catalog repositories (mirroring
playback), implemented as a single user-scoped cascade delete of the user's tracks
(and their audio objects) and playlists. Until then, this decision to omit it is
the retention policy of record.
