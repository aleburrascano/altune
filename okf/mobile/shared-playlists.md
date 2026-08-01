---
type: Subsystem
title: Shared playlists
description: Every playlist write in the app plus the playlist picker, extracted to shared/ once two features needed it — batch-only membership, and a resolveTrackIds thunk so a track can be saved on pick.
resource: apps/mobile/src/shared/playlists/
tags: [mobile, shared, playlists, mutations, optimistic-updates]
---

Playlist writes used to live in `features/library/hooks/usePlaylistMutations.ts`, with the picker and the create-modal beside them in `features/library/ui/`. Issue #18 — "when you save a track for the first time, offer adding it straight to a playlist" — put a second consumer on them: `features/detail`. Features may not import each other, so the module moved to `shared/playlists/` (2026-08-01). The whole module moved, including `useRenamePlaylist` / `useDeletePlaylist`, which still have only the one consumer. Splitting it would have put the optimistic-patch/rollback/alert/invalidate policy in two files, and that policy living in exactly one place is the point of the library rule that names it. The unit that earns a place in `shared/` is the module, not each hook.

## Batch-only membership

Every membership hook takes a list. `useAddTracksToPlaylist` mutates on `{playlistId, trackIds}`, `useRemoveTracksFromPlaylist(playlistId)` on `string[]`, and `useCreatePlaylistWithTracks` on `{name, trackIds}`. There is no single-track variant, and a caller with one track passes a one-element array: `removeMut.mutate([track.id])`. A `useAddTrackToPlaylist(trackId)` alongside the plural form would be two code paths with two optimistic-patch implementations to keep in step, and the single-track case is the plural case with `length === 1`.

The server decides how many tracks actually landed, and the mutation reports that rather than assuming the request size. `addTracksToPlaylist` answers `{added, skipped}`; when `added < trackIds.length` the hook alerts with the count and the playlist name ("2 tracks were already in Focus."). This is the client half of the backend's skip-don't-fail contract — see [catalog playlist](../backend/catalog/playlist.md#batch-membership-2026-08-01). The optimistic patch bumps `track_count` by the *requested* size because that is all it knows at `onMutate` time; the `onSettled` invalidation of `playlistKeys.list` is what corrects an over-count after a skip, so the optimism is bounded by a refetch rather than by a guess.

`useCreatePlaylistWithTracks` deliberately treats a failed add as a *success* with a note, not an error: the playlist was created, and rolling it back would throw away work the user asked for. Its `mutationFn` swallows the add failure and returns `addFailed`, so the created playlist survives in `data` and the caller's `onSettled` still closes the modal.

## The resolveTrackIds seam

`AddToPlaylistSheet` takes `resolveTrackIds: () => Promise<string[]>` rather than a `trackIds: string[]`. The reason is the detail screen: when a user taps "Add to Playlist" on a discovery result they do not own yet, the track has no server id, and the optimistic placeholder id `useSaveTrack` writes into the cache is not addressable by the API. The sheet must not add anything until a real id exists.

A thunk is what makes that expressible without a union type or a mode flag. Callers with ids already in hand write `() => Promise.resolve(selection.ids)`; the detail screen writes `async () => [owned?.trackId ?? (await save.mutateAsync(req)).id]`. The save-then-add ordering lives with the caller that knows about saving, and the sheet stays ignorant of tracks entirely — it knows playlists and a promise of ids.

The thunk runs on pick, never on open, so opening the sheet on an unsaved track saves nothing until a playlist is chosen; backing out costs the user nothing. A rejected thunk closes the sheet and never calls the add endpoint — a failed save must not be followed by an add against an id that does not exist. `resolving` folds into the same `busy` flag as the two mutations, so the row list is inert for the whole save-then-add sequence rather than only its second half.

## Cache keys

Every hook reads keys from `playlistKeys` in `@shared/lib/query-keys`. `onSettled` invalidates both `playlistKeys.list` and `playlistKeys.detail(playlistId)` on membership writes, because a change to a playlist's contents alters the grid's `track_count` and preview artwork as well as the open detail screen. The SSE events (`tracks_added_to_playlist`, `tracks_removed_from_playlist`) invalidate and patch the same two families — see [shared-events](shared-events.md) — so a write is reconciled whether the acting device or another one made it.
