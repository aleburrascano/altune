`library` owns what a user's library holds: tracks, playlists, featured artists and their audio.

It owns the tables `featured_artists`, `orphaned_audio`, `playlist_tracks`, `playlists`, `track_featured_artists`, `track_identity`, `track_metadata_changes`, `tracks`. Only `catalog` should write them, and other modules may read them. `acquisition` changes the status and `failure_reason` of a track only through the ledger port, which `library` implements, and keeps its own job queue and upgrade bookkeeping in its own tables, `acquisition_jobs` and `acquisition_upgrade_state`. Until the old columns are dropped, the `acquisition_*` job columns and `rejected_source_keys` of `tracks` are copies kept in step by triggers for a rollback; only `UpdateFenced` still reads `acquisition_attempts`. The queue and the upgrade bookkeeping no longer bump `version`; only the ledger port and the in-place verdict write do, so catalog's optimistic locking sees catalog and ledger writes.

## Who reads our tables

Other modules read these tables with SQL of their own instead of through a port. Each reader is listed here with the columns it reads, so a change to a column knows who to tell. `scripts/library-table-readers.sh` fails when a file outside `library` reads one of the tables above and is not listed, or when a listed file stops reading them.

- `services/go-api/internal/download/internal/adapters/persistence/job_queue.go` reads `tracks.id`, `user_id` and `acquisition_status`, and takes row locks on `tracks` (`FOR UPDATE`) to serialize job claims with ledger writes.
- `services/go-api/internal/download/internal/adapters/persistence/upgrade_store.go` reads `tracks.id`, `user_id`, `acquisition_status`, `acquisition_evidence`, `audio_ref` and `audio_source_url`, and takes `FOR NO KEY UPDATE` locks on `tracks`.
- `services/go-api/internal/download/internal/adapters/persistence/outcome_store.go` reads `tracks.id` to find outcomes whose track is gone.
- `services/go-api/internal/download/internal/adapters/persistence/rejection_store.go` reads `tracks.id` to find rejections whose track is gone.
- `services/go-api/internal/search/internal/adapters/persistence/related_tracks_repo.go` reads `tracks.user_id`, `title`, `artist`, `album`, `album_artist`, `artwork_url` and `added_at`.
