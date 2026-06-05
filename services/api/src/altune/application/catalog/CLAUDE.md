# application/catalog — bounded-context local context

Use cases + ports for the catalog context. Imports `domain/` + stdlib only; **never** `adapters/` or framework code (enforced by `.claude/rules/application-layer.md`). Ports are defined here; adapters implement them.

## Ports

- **TrackRepository** (`ports.py`) — `Protocol` over the `Track` aggregate.
  - `list_for_user(user_id, limit, offset) -> (Sequence[Track], total)` — one page ordered `(added_at DESC, id DESC)`; user-scoped at the storage boundary.
  - `add(track) -> (Track, created)` — persist, or return the existing track on a dedup hit (`created=False`). Idempotency is the DB `UNIQUE(user_id, dedup_key)` constraint + `ON CONFLICT`, **never** a read-then-write check (which races). The repo computes the dedup key itself from the track's fields.

## Use cases (one file each)

- **ListTracks** (`list_tracks.py`) — read path behind `GET /v1/tracks`.
- **AddTrackToLibrary** (`add_track_to_library.py`) — write path behind `POST /v1/tracks`.
- **CreatePlaylist** (`create_playlist.py`) — `POST /v1/playlists`.
- **ListPlaylists** (`list_playlists.py`) — `GET /v1/playlists`.
- **GetPlaylist** (`get_playlist.py`) — `GET /v1/playlists/:id` with full track list.
- **RenamePlaylist** (`rename_playlist.py`) — `PATCH /v1/playlists/:id`.
- **DeletePlaylist** (`delete_playlist.py`) — `DELETE /v1/playlists/:id`.
- **AddTrackToPlaylist** (`add_track_to_playlist.py`) — `POST /v1/playlists/:id/tracks`.
- **RemoveTrackFromPlaylist** (`remove_track_from_playlist.py`) — `DELETE /v1/playlists/:id/tracks/:track_id`.
- **ReorderPlaylistTracks** (`reorder_playlist_tracks.py`) — `PATCH /v1/playlists/:id/tracks/reorder`.

## Conventions

- Use cases receive ports via `__init__`; no global state. The use case is the unit-of-work boundary (the HTTP adapter commits the session).
- Keep ports aggregate-scoped (`list_for_user`, `add`) — no god-repository, no ORM leakage.
