`library` owns what a user's library holds: tracks, playlists, featured artists and their audio.

It owns the tables `featured_artists`, `orphaned_audio`, `playlist_tracks`, `playlists`, `track_featured_artists`, `tracks`. Only `catalog` should write them, and other modules may read them. `acquisition` changes the status and `failure_reason` of a track only through the ledger port, which `library` implements, and keeps its own job queue and upgrade bookkeeping in its own tables, `acquisition_jobs` and `acquisition_upgrade_state`. Until the old columns are dropped, the `acquisition_*` job columns and `rejected_source_keys` of `tracks` are copies kept in step by triggers for a rollback; only `UpdateFenced` still reads `acquisition_attempts`. The queue and the upgrade bookkeeping no longer bump `version`; only the ledger port and the in-place verdict write do, so catalog's optimistic locking sees catalog and ledger writes.

## Must not

- Import another feature. `download`, `search` and `playback` reach `library` only through `app/bridges`, and `library` reaches search's featured-artist credits through `app/bridges/libraryfromsearch`.
- Read config or the environment. `app` hands it `Deps`.
- Put a domain type on the wire. Wire shapes live only in `internal/adapters/handler/dto.go`, and only `internal/adapters/handler/errors.go` maps domain errors to `apierror` codes. The `track_added_to_library` event payload is converted to its wire shape in `internal/adapters/handler/event_wire.go`.
- Let `domain` import logging, the clock or `apierror`. Time comes in as a parameter.

## Shape

The door is `library.go`: `New(Deps)`, `Deps` (infrastructure only: the database, its call metrics, the events publisher and the other outside parts, never a repository, because `New` builds its own), `Module` with `Routes()` (the `Bounded` group for the API request timeout, the `LongLived` group for the audio stream), and the names `app`, the bridges and `cmd` use. Everything else is under `internal/`:

- `domain/`: one file per entity or value object, and `errors.go` with every error the module defines.
- `ports/ports.go`: every interface the module needs from outside.
- `service/`: one file per use case; the library lenses are the read-model use case (`library_lenses.go`).
- `adapters/handler/`: `routes.go`, `dto.go`, `errors.go`, `event_wire.go` and the rate limits.
- `adapters/persistence/` and `adapters/memory/`: one repository per table, Postgres and memory side by side; `contracttest/` holds the suites that run against both (the Postgres half runs with `INTEGRATION=1`).
- `adapters/storage/`: the audio files, on a filesystem or in object storage.
- `adapters/telemetry/`: the metrics registry and its live snapshot.
- `librarytest/` stays at the root because `app`, its bridges and `cmd` import its builders and fakes in their tests. A door name only tests use lives there too.

## Who reads our tables

Other modules read these tables with SQL of their own instead of through a port. Each reader is listed here with the columns it reads, so a change to a column knows who to tell. `scripts/library-table-readers.sh` fails when a file outside `library` reads one of the tables above and is not listed, or when a listed file stops reading them.

- `services/go-api/internal/download/internal/adapters/persistence/job_queue.go` reads `tracks.id`, `user_id` and `acquisition_status`, and takes row locks on `tracks` (`FOR UPDATE`) to serialize job claims with ledger writes.
- `services/go-api/internal/download/internal/adapters/persistence/upgrade_store.go` reads `tracks.id`, `user_id`, `acquisition_status`, `acquisition_evidence`, `audio_ref` and `audio_source_url`, and takes `FOR NO KEY UPDATE` locks on `tracks`.
- `services/go-api/internal/download/internal/adapters/persistence/outcome_store.go` reads `tracks.id` to find outcomes whose track is gone.
- `services/go-api/internal/download/internal/adapters/persistence/rejection_store.go` reads `tracks.id` to find rejections whose track is gone.
- `services/go-api/internal/search/internal/adapters/persistence/related_tracks_repo.go` reads `tracks.user_id`, `title`, `artist`, `album`, `album_artist`, `artwork_url` and `added_at`.
