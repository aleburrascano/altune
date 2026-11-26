`library` owns what a user's library holds: tracks, playlists, featured artists and their audio.

It owns the tables `featured_artists`, `orphaned_audio`, `playlist_tracks`, `playlists`, `track_featured_artists`, `tracks`. Only `catalog` should write them, and other modules may read them. One writer still breaks that rule. `acquisition`'s persistence adapter updates the `acquisition_*` columns of `tracks`, along with `failure_reason`, `rejected_source_keys` and `version`, and that last one is what catalog's optimistic locking relies on.
