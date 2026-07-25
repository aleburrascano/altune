# Catalog context — router

The Track and Playlist aggregate context: a user's owned music metadata, dedup, playlist ordering, and audio file storage/streaming.

Invariants:

- The `AcquisitionStatus`/`AudioRef`/`FailureReason` invariant is enforced only through aggregate methods (`MarkReady`/`MarkFailed`/`RevertToPending`/`IsStreamable`) — never direct field mutation.
- `NewTrack` computes a `DedupKey`; the persistence upsert is `ON CONFLICT (user_id, dedup_key) DO NOTHING`, so re-saving by metadata is idempotent (`TrackRepository.Add` returns `(stored, created, err)`).
- `Playlist` positions are contiguous 0..N-1 with no duplicate tracks, enforced only via `AddTrack`/`RemoveTrack`/`Reorder`.
- Acquisition is reached only through the `AcquisitionScheduler` port — catalog never imports acquisition. Discovery is reached only through `adapters/discoverybridge` (mirroring `playback/catalogbridge`), never a direct import.

## Audio storage and streaming

- **`AudioContentType` is the single source of MIME truth** for both the upload side (object storage sets it on `PutObject`) and the serve side (the proxy stream endpoint labels the response). The two must agree: iOS/expo-audio decodes progressive audio by `Content-Type`, so an m4a served as `audio/mpeg` fails to play. Legacy mp3 refs default to `audio/mpeg`.
- `AudioURLSigner` is an **optional** capability, detected by type assertion and never required. Object storage satisfies it via presigned GET; the filesystem store doesn't, and callers fall back to the proxy stream endpoint. Presigning is a local HMAC over the request — no network call to storage — so it's cheap enough to mint per track at queue-build time. `maxAudioURLBatch` still caps one request so a queue can't sign thousands of objects.
- Tracks that can't be signed (unknown, not owned, not ready, no signer) are simply **absent** from the resolve response; the client streams those through the proxy. `HandleRecover` is the client's error hook for presigned streams, which bypass the proxy's missing-file recovery — idempotent, and a no-op when the file is really there.
- `FilesystemAudioStore.Store` falls back to `copyThenRemoveAcrossFilesystems` on `EXDEV`: the temp dir and the audio volume are usually separate mounts on a Linux VM, and without this every acquisition fails. `Stream` returns a handle the caller **must** close.
- Path-traversal tests only assert backslash rejection on Windows — on Linux `..\windows\system32` is a single valid filename and correctly not traversal.

## Read-side projections and shared SQL

`trackColumns` / `trackColumnsPrefixed` (the `t.`-aliased form for joins), `playlistTrackCountSubquery`, `renumberPlaylistPositions` and `trackScanDest` each exist so a rule has exactly one definition and cannot drift between the queries that share it. `renumberPlaylistPositions` in particular is shared by every write path that can drop a `playlist_tracks` row — playlist track removal here, track deletion in `track_repo.go`.

`track_count` and `preview_artwork` are computed in the same query as the playlist rows, so the playlists screen is one round-trip with no per-playlist follow-up; `track_count` is projected on `GetByID` too so single-playlist responses (rename) report a real count without loading tracks. `PreviewArtworkLimit` is the one definition shared by the SQL projection and the Go fallback (`handler.previewArtworkFromTracks`) — two independent implementations of the same selection rule.

`GetByID` and `ListByIDs` deliberately **do not** load `FeaturedArtists`: every current caller reads only status/audio-ref fields (the hot one is audio-URL presigning), so the join isn't paid on every call. `SetTrackNumber` is fill-only (`WHERE track_number IS NULL`) so it never clobbers a real value and is safe to call repeatedly. On a dedup-key conflict `Add` returns the existing track, so the caller needs no second lookup.

`FeaturedArtist.IdentityKey` (MBID, else Deezer id, else normalized name) mirrors the generated column on the `featured_artists` table so Go-side and SQL-side identity agree. `TrackResponse` is `service.TrackDTO` so the `track_added_to_library` event payload and the HTTP response can never drift. `SourceURL` on the add-track request is not persisted — it rides through to the acquisition scheduler so a directly-downloadable source is grabbed exactly rather than re-searched by metadata.

Knowledge base: `okf/backend/catalog/index.md`; tables in `okf/data/tracks-table.md`, `okf/data/playlists-table.md` — read before structural work; update in the same commit when behavior they describe changes (pre-commit hook enforces).
