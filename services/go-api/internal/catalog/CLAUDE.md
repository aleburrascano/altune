# Catalog context — router

The Track and Playlist aggregate context: a user's owned music metadata, dedup, playlist ordering, and audio file storage/streaming.

Invariants:

- The `AcquisitionStatus`/`AudioRef`/`FailureReason` invariant is enforced only through aggregate methods (`MarkReady`/`MarkFailed`/`RevertToPending`/`IsStreamable`) — never direct field mutation.
- `NewTrack` computes a `DedupKey`; the persistence upsert is `ON CONFLICT (user_id, dedup_key) DO NOTHING`, so re-saving by metadata is idempotent (`TrackRepository.Add` returns `(stored, created, err)`).
- `Playlist` positions are contiguous 0..N-1 with no duplicate tracks, enforced only via `AddTrack`/`RemoveTrack`/`Reorder`.
- Acquisition is reached only through the `AcquisitionScheduler` port — catalog never imports acquisition.

Knowledge base: `okf/backend/catalog/index.md`; tables in `okf/data/tracks-table.md`, `okf/data/playlists-table.md` — read before structural work; update in the same commit when behavior they describe changes (pre-commit hook enforces).
