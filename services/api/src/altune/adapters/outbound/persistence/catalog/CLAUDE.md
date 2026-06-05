# adapters/outbound/persistence/catalog — local context

Postgres persistence for the catalog context. Implements the `application/catalog` `TrackRepository` port against an `AsyncSession` (asyncpg). Domain objects never see SQLAlchemy; this layer owns the row↔aggregate mapping.

## Files

- **track_row.py** — `TrackRow` SQLAlchemy model for the `tracks` table + `from_domain`/`to_domain`. Carries: `artwork_url`, `acquisition_status` (server_default `'pending'`), `dedup_key`, and the `import-legacy-library` columns: `year`, `genre`, `track_number`, `album_artist`, `isrc`, `audio_ref` (all nullable).
  - `# AIDEV-NOTE:` `Mapped[T]` annotations resolve at runtime, so `UUID`/`datetime` are imported at module scope (NOT under `TYPE_CHECKING`).
  - `# AIDEV-NOTE:` `_dedup_key_default` is a column-level default that recomputes `dedup_key` from title/artist/album for any raw `TrackRow(...)` insert that omits it (e.g. test seeding) — `from_domain()` sets it explicitly; this is the safety net.
  - `# AIDEV-NOTE:` `dedup_key` is persistence-only — derived from the domain normalizer, never a `Track` field.
- **track_repository.py** — `SqlAlchemyTrackRepository`.
  - `add(track)`: `INSERT ... ON CONFLICT (user_id, dedup_key) DO NOTHING RETURNING id`; an empty RETURNING means the row already existed (`created=False`). Either way it `SELECT`s the canonical row back. The dedup key is computed here via the domain `dedup_key(...)`.
  - `list_for_user(...)`: paged `WHERE user_id` ordered `(added_at DESC, id DESC)` + a `COUNT(*)` total.
- **playlist_row.py** — `PlaylistRow` + `PlaylistTrackRow` SQLAlchemy models for `playlists` and `playlist_tracks` tables. `from_domain`/`to_domain` on `PlaylistRow`.
- **playlist_repository.py** — `SqlAlchemyPlaylistRepository`. Full CRUD + track management (add, remove, reorder) + preview artwork (up to 4 URLs) + `get_track_count`. `list_for_user` returns playlists with empty tracks tuple (listing doesn't need them); track counts are queried separately via `get_track_count`.

## Gotchas

- The `UNIQUE(user_id, dedup_key)` constraint lives in the alembic migration (`migrations/versions/a1c4e7b9d2f3_*`), **not** the `TrackRow` model. Tests that build schema via `Base.metadata.create_all` therefore lack the constraint; the write-path e2e test uses an alembic-migrated container instead. The read-path tests keep `create_all` so they can seed several arbitrary rows per user.
- The session is supplied by the HTTP layer and committed there; the repository never commits.
