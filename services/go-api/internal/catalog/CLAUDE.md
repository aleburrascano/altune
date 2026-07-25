# Catalog context — router

The Track and Playlist aggregate context: a user's owned music metadata, dedup, playlist ordering, and audio file storage/streaming.

Layout:

- `domain/` — `Track`, `Playlist`, `FeaturedArtist`, `CodedError`.
- `ports/` — `TrackRepository`, `PlaylistRepository`, `AudioStore`, `AudioURLSigner`, `AudioLister`, `AcquisitionScheduler`, `FeaturedArtistResolver`.
- `service/` — track/playlist use cases, audio-URL resolution, streaming, featured backfill.
- `adapters/` — `persistence/` (pgx repos), `storage/` (filesystem + object storage), `handler/`, `discoverybridge/`.
- `catalogtest/` — in-memory fakes shared by the service and handler test packages.

## Rules

- Change `AcquisitionStatus` / `AudioRef` / `FailureReason` only through aggregate methods, never by direct field mutation.
- Change `Playlist` positions only through `AddTrack` / `RemoveTrack` / `Reorder`; they stay contiguous 0..N-1 with no duplicates.
- Never import acquisition — go through the `AcquisitionScheduler` port.
- Never import discovery — go through `adapters/discoverybridge`.
- Keep `AudioContentType` the only place a stored ref maps to a MIME type; upload and serve sides must agree.
- Never require `AudioURLSigner` or `AudioLister` — detect them by type assertion and degrade.
- Never let a batch resolve sign an unbounded number of objects.
- Never `os.Rename` audio into place without the `EXDEV` fallback.
- Always close the handle `Stream` returns.
- Keep `PreviewArtworkLimit`, `trackColumns`, `playlistTrackCountSubquery`, `renumberPlaylistPositions` and `trackScanDest` single-definition — every sharer reads the same one.
- Never add the `FeaturedArtists` join to `GetByID` / `ListByIDs`; their callers don't need it.
- `SetTrackNumber` is fill-only — never clobber an existing value.
- Keep `FeaturedArtist.IdentityKey` in step with the generated column on `featured_artists`.
- Never let the wire response and the `track_added_to_library` payload diverge — both are `service.TrackDTO`.

Why each rule exists: `okf/backend/catalog/index.md`; tables in `okf/data/tracks-table.md` and `okf/data/playlists-table.md` — read before structural work; update in the same commit when behavior they describe changes (pre-commit hook enforces).
