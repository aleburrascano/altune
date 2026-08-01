# Catalog context — router

The Track and Playlist aggregate context: a user's owned music metadata, dedup, playlist ordering, and audio file storage/streaming.

Layout:

- `domain/` — `Track`, `Playlist`, `FeaturedArtist`, `CodedError`, the library read-models (`AlbumGroup`, `ArtistGroup`, `LibraryQuery`, `LibrarySort`, `OwnedTrackRef`).
- `ports/` — `TrackRepository`, `PlaylistRepository`, `AudioStore`, `AudioURLSigner`, `AudioLister`, `AcquisitionScheduler`, `FeaturedArtistResolver`.
- `service/` — track/playlist use cases, audio-URL resolution, streaming, featured backfill, `LibraryLensService`.
- `adapters/` — `persistence/` (pgx repos), `storage/` (filesystem + object storage), `handler/`, `discoverybridge/`.
- `catalogtest/` — in-memory fakes shared by the service and handler test packages.

## Rules

- Change `AcquisitionStatus` / `AudioRef` / `AudioVersion` / `FailureReason` / `RejectedSourceKeys` only through aggregate methods, never by direct field mutation.
- Re-stamp `AudioVersion` on every write of the audio, never reuse the previous token.
- Never derive a client-facing audio version from a clock.
- Change `Album` only through `SetAlbum` — it recomputes `DedupKey`, and the two may never drift.
- A blank album resolves to the track's title; apply that in the aggregate, never at the API edge.
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
- Albums and Artists are SQL groupings, never derived by a caller — `ListAlbumsForUser` / `ListArtistsForUser` are the only producers.
- Filter and sort tracks in SQL through `LibraryQuery`; never hand a caller the whole library to sort.
- Reject `sort=year` for artists rather than silently falling back.
- Keep `FailureMessage` the one map from a failure reason to human copy.

Why each rule exists: `okf/backend/catalog/index.md`; tables in `okf/data/tracks-table.md` and `okf/data/playlists-table.md` — read before structural work; update in the same commit when behavior they describe changes (pre-commit hook enforces).
