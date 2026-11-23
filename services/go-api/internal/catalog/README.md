`catalog` owns what a user's library holds: tracks, playlists, featured artists and their audio.

It owns the tables `featured_artists`, `orphaned_audio`, `playlist_tracks`, `playlists`, `track_featured_artists`, `tracks`. Only `catalog` should write them, and other modules may read them. Two writers still break that rule: `acquisition` updates the queue and upgrade columns of `tracks` in its persistence adapter, and the `cmd/library*` tools update `tracks` directly. Those writes move behind a `catalog` port.
