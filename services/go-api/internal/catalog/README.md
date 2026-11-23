`catalog` owns what a user's library holds: tracks, playlists, featured artists and their audio.

It owns the tables `featured_artists`, `orphaned_audio`, `playlist_tracks`, `playlists`, `track_featured_artists`, `tracks`. Only `catalog` writes them. Other modules may read them.
