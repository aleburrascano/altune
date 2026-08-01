# shared/playlists — router

Playlist writes and the playlist picker, shared by `features/library` (list, playlist detail, selection mode) and `features/detail` (save a discovery result straight into a playlist).

Layout:

- `mutations.ts` — every playlist write: `useCreatePlaylist`, `useCreatePlaylistWithTracks`, `useAddTracksToPlaylist`, `useRenamePlaylist`, `useDeletePlaylist`, `useRemoveTracksFromPlaylist`.
- `AddToPlaylistSheet.tsx` — the picker; takes a `resolveTrackIds` thunk so a caller can save an unowned track before the add.
- `CreatePlaylistModal.tsx` — name entry, used standalone and from the sheet.
- `index.ts` — the barrel every consumer imports from.
- `__tests__/` — `mutations`, `AddToPlaylistSheet`.

Dependencies: `@shared/api-client/playlists`, `@shared/lib/query-keys`, `@shared/ui`, `@tanstack/react-query`. No feature imports.

## Rules

- Route every playlist write through `mutations.ts` — it owns the optimistic-patch/rollback/alert/invalidate policy; screens keep only UI state.
- Every membership write is a batch: one call carries N track ids, never a loop of single-track calls.
- A track already in the target playlist is skipped by the server, never an error — report the skipped count, never fail the batch.
- Resolve track ids through the sheet's `resolveTrackIds` thunk; the sheet never saves a track itself.
- Import cache keys from `playlistKeys` in `@shared/lib/query-keys`; never retype a key literal.

Why each rule exists: `okf/mobile/shared-playlists.md` — read before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).
