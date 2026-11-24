# Catalog — Module Architecture

The catalog bounded context (`services/go-api/internal/catalog/`) owns a user's
library: tracks, playlists, featured artists, and the audio objects that back ready
tracks. Every row is user-scoped (`shared.UserId`). Acquisition is a customer of
catalog: it consumes `domain.Track` and its `MarkReady` / `MarkFailed` /
`RevertToPending` invariants.

The module root holds only the four layers, `catalogtest/` and this file.

## Layering

```
catalog/
├── domain/                      aggregates and value objects, no I/O
│   ├── track.go                 Track aggregate and its status transitions
│   ├── failure.go               acquisition failure classification
│   ├── playlist.go              Playlist aggregate and membership
│   ├── featured_artist.go       featured-artist value objects
│   ├── library_lens.go          library lens (filtered views of the library)
│   ├── dedup.go                 duplicate-track detection key
│   ├── source_url.go            validated source URL value
│   ├── repo_error.go            ErrDBTransient and WrapRepoError, the 503 mapping
│   └── coded_error.go           the domain sentinels, as shared/domain coded errors
├── ports/                       interfaces the services depend on
│   ├── track_repo.go            track persistence
│   ├── playlist_repo.go         playlist persistence
│   ├── audio_store.go           audio object storage
│   ├── audio_store_metrics.go   audio-store counters (incl. orphaned delete)
│   ├── orphaned_audio.go        orphaned-audio recorder and queue
│   ├── acquisition_scheduler.go hand-off to acquisition
│   ├── featured_resolver.go     featured-artist resolution
│   ├── db_call_metrics.go       database call metrics
│   └── db_errors.go             database error classes
├── service/                     use cases
│   ├── add_track.go             save a track and schedule acquisition
│   ├── delete_track.go          delete a track row, then its audio object
│   ├── reconcile_orphaned_audio.go  retry deletion of orphaned audio
│   ├── track_reads.go           list a user's tracks and read one track's status
│   ├── set_track_number.go      set a track number
│   ├── stream_track.go          stream a track's audio
│   ├── audio_url.go             audio URL issuing
│   ├── playlist_lifecycle.go    playlist CRUD
│   ├── playlist_membership.go   add/remove/reorder playlist tracks
│   ├── library_lenses.go        library lens queries
│   ├── featured_artist.go       featured-artist backfill, its admission gate, tracks featuring an artist
│   ├── track_dto.go             track DTO mapping
│   ├── options.go               service options
│   └── errors.go                service errors
├── adapters/
│   ├── handler/                 HTTP handlers (tracks, playlists, library, stream, audio URL, featured artists) and their rate limits
│   ├── persistence/             Postgres repositories and connection pool
│   ├── storage/                 audio store: filesystem, object storage, scoped store
│   └── metrics/                 expvar metrics
└── catalogtest/                 in-memory fakes for catalog ports

```

## Database outages: 503 `catalog.temporarily_unavailable`

When the catalog database refuses, drops or stalls a call, the routes below answer
503 with code `catalog.temporarily_unavailable` and detail `catalog temporarily
unavailable`. Every other database error answers 500 `internal`, so a client can
tell an outage from a bug.

- **What is transient.** The Postgres repositories (`internal/adapters/persistence/pool.go`)
  name an error `ErrDBTransient` when `database.IsTransient` says the database is
  unavailable: a connect fails, the connection is reset, dropped or already closed,
  a call runs past its deadline (each call gets 5s, `dbCallTimeout`, and a caller's
  own deadline counts too), or Postgres answers with a retryable SQLSTATE.
  A caller's cancel is not transient, and neither is any other error, such as a
  constraint violation.
- **One mapping.** A service passes a repository error through
  `domain.WrapRepoError(ctx, op, err)`. A transient error becomes
  `ErrCatalogTemporarilyUnavailable` and logs `catalog.db_transient` at WARN with
  `op` and the repository error, the one place that error is recorded. The
  returned error carries only `op` and the sentinel, so nothing behind the 503
  can change the response.
  Any other error keeps its chain and adds no status. A plain error answers 500
  through `httputil.HandleServiceError`, and a repository error that carries a
  status keeps it: a reorder that loses a race to another edit answers 400
  (`ErrPlaylistChangedDuringReorder`).
- **Routes that answer it:**
  - `GET /tracks`, `POST /tracks` (the library-size count and the insert),
    `GET /tracks/{trackId}/status`, `PATCH /tracks/{trackId}/track-number`,
    `DELETE /tracks/{trackId}` and `GET /tracks/featuring`;
  - `POST /audio-urls`, `GET /tracks/{trackId}/audio` and
    `POST /tracks/{trackId}/audio/recover`;
  - `GET /library/albums` and `GET /library/artists`;
  - every `/playlists` route.
- **Still 500 during an outage.** These wrap the error with `fmt.Errorf`:
  - `POST /tracks/featured-backfill`, at its first database call, `ListForUser`
    in `internal/core/featured/featured_artist.go`. A failing repository stub
    reaches it;
  - the ownership lookup that `PATCH /tracks/{trackId}/track-number` runs when no
    row was updated, and the write that marks a track's audio missing in
    `POST /tracks/{trackId}/audio/recover`. Each is the second database call of
    its request, so only a fault between two calls reaches it.
- **No `Retry-After`.** `httputil` sets the header only for an error that carries a
  retry time, as the rate limiters' errors do. A database outage has no honest
  retry time: it can last a second or an hour, and a made-up value would tell
  clients to retry on a schedule the server never promised.
- **Tests.** `TestCatalogRoutes_OnlyATransientRepositoryErrorAnswersTemporarilyUnavailable`
  pins 500 and 503, and the `catalog.db_transient` line with its `op` and the
  repository error, on the list, add, delete, track-number and audio-URL routes.
  The service tests
  `TestHotPaths_ClassifyTransientDBFailures` and
  `TestPlaylistAndLibraryPaths_ClassifyTransientDBFailures` pin both on stream,
  recover and status; playlist list, get, rename, add track and reorder; the two
  library lenses; and featuring.
  `TestPlaylistMembershipService_Reorder_RefusesAPlanInvalidatedAfterTheRead`
  pins the 400 that passes through.

## Data retention & deletion

This document records the retention/deletion story for catalog's personal data and
the explicit design decision about a bulk per-user erasure path. It is the written
companion to the playback module's erasure precedent
(`internal/playback/internal/core/ports/queue_state.go`, `DeleteForUser`).

### What personal data catalog holds

- **Track rows** — a user's library metadata (title, artist, album, featured
  artists, acquisition status, audio reference).
- **Playlist rows** and their track membership.
- **Audio objects** — the actual audio files in the audio store (S3 / filesystem),
  addressed by the `audio_ref` carried on a ready track.

Every catalog row is user-scoped (`shared.UserId`), so all catalog PII is
partitioned by user.

### Deletion path (present tense)

Deletion is **per item**, driven by the caller:

- `TrackDeleter.Delete(ctx, id, userId)` removes one track row and returns its
  `audioRef`. Playlist membership is removed by the database cascade (see #415), so
  deleting a track needs no separate membership cleanup.
- `PlaylistRepository.Delete(ctx, id, userId)` removes one playlist.
- After the track row is deleted, `DeleteTrackService.Execute` deletes the audio
  object through `AudioStore.Delete`.

#### Partial-deletion handling (audio orphans)

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

Reconciliation (#1058): the delete path also records the orphan durably in the
`orphaned_audio` table (migration 021) through `OrphanedAudioRecorder`, and the
leader-gated `orphaned audio reconcile` background job
(`ReconcileOrphanedAudioService`, every 10 minutes) retries the audio-store
delete until the object is gone, recording each failed attempt on the row.

The sweep never deletes an object a track still references. Storage keys are
shared by tracks with equivalent metadata and a fresh acquisition rewrites the
same canonical key, so immediately before each delete it checks, in one query,
whether any track (of any user) references the key (the orphan is then dropped
from the queue without deleting) or the owner has a pending acquisition (the
orphan stays queued and is re-checked next run). A failed check aborts the run.

Until migration 021 is applied the record write fails with SQLSTATE 42P01: the
delete keeps its prior outcome (log line, counter, `ErrAudioOrphaned`) and the
sweep idles.

### Decision: no dedicated bulk per-user erasure method (for now)

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
