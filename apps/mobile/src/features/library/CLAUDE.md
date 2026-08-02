# library — feature-local router

Single chip-filtered Library screen (`docs/superpowers/specs/2026-06-28-library-redesign-design.md`). Opens to Playlists; a persistent search + chip bar swaps one focused view at a time. `LibraryChip` is the active view: `'playlists' | 'tracks' | 'albums' | 'artists'`.

Layout:

- `ui/LibraryScreen.tsx` — orchestrator; owns `chip`, per-chip `sortByChip`, search, the track action sheet, selection mode, and the loading/error/empty branches.
- `ui/LibraryChips.tsx`, `ui/SortControl.tsx`, `ui/LibraryNoResults.tsx`.
- `ui/PlaylistsGrid.tsx`, `ui/TracksList.tsx`, `ui/AlbumsGrid.tsx`, `ui/ArtistsGrid.tsx`, `ui/LibraryRow.tsx`.
- `ui/PlaylistDetailScreen.tsx` / `ui/PlaylistHero.tsx` — route `/library/playlist/[id]`. `ui/AddTracksToPlaylistModal.tsx` — the library picker that fills a playlist in bulk.
- `useSelection.ts` — multi-select state for a track list. `ui/SelectionBar.tsx` — the bar and its `SelectionAction` type. `ui/selectionActions.ts` — `buildSelectionActions`.
- `ui/trackMenu.ts` — `buildTrackMenuItems`. `ui/sort.ts` — the `*_SORT_OPTIONS` label lists; the sort keys are wire values the server applies.
- `hooks/useLibraryHome.ts` — `useLibraryTracks` (infinite), `useLibraryAlbums`, `useLibraryArtists`, one query per chip. `hooks/usePlaylistActions.ts`, `hooks/useLibrarySearch.ts` (debounce only), `hooks/useDeleteTrack.ts` (`useDeleteTrack` / `useDeleteTracks`), `hooks/useRetryAcquisition.ts` (failed tracks), `hooks/useReacquireTrack.ts` (replace the audio of a ready track). `state.ts` — `_viewForState`.
- `__tests__/` — `useSelection`; the rest is rebuilt per `okf/playbooks/test-taxonomy.md`.

Dependencies: `@shared/ui` (plus `primitives/{ActionSheet,Artwork,SearchBar}` directly — native deps, structure audit F2), `@shared/api-client/library`, `@shared/lib/{format,detail-handoff,query-keys}`, `@shared/playback`, `@shared/playlists`, `@shared/offline/pinnedStore`.

## Rules

- The noun is **Track**, never "Song" — chip and list vocabulary included.
- Re-acquire patches the cache on success only; a playing track must never be shown pending before the server accepts.
- A failed re-acquire restores the track to ready — it never renders a playable track as broken.
- Keep "Download" meaning offline pinning; audio replacement is "Re-acquire".
- Import cache keys from `libraryKeys` / `playlistKeys` in `@shared/lib/query-keys`; never retype a key literal.
- Route every playlist write through `@shared/playlists` — it owns the optimistic-patch/rollback/alert/invalidate policy; screens keep only UI state.
- Assemble the track context menu only in `ui/trackMenu.ts`, and the selection bar's actions only in `ui/selectionActions.ts`.
- Hold multi-select state only in `useSelection`; a screen never keeps its own selected-id array.
- Act on a selection in one request per action — never a loop of single-track requests, except library deletion, which has no batch endpoint and reports how many of the requested tracks it removed.
- Offer selection mode only on track lists — never on the Albums or Artists grids.
- Confirm a destructive bulk action before it runs, and name the count in the prompt.
- Send `q` and `sort` to the server; never filter or sort a library list in JS.
- Playing from the Tracks chip queues the **whole** collection via `tracksState.loadAll()` — never the loaded pages, which would silently scope shuffle to what the user happened to scroll.
- Every chip's query keeps its previous results while a new `q`/`sort` is in flight (`placeholderData: keepPreviousData`); a search must never drop the screen into the full-page skeleton.
- Read albums and artists from `/v1/library/*`; never regroup a track list on the device.
- Keep each chip on its own query, enabled only while that chip is active.
- Never show the empty-library CTA when a search has merely filtered the view to zero.
- Navigate to detail through `useLibraryNavigation` + the handoff seam.

Why each rule exists, and what is deliberately deferred: `okf/mobile/library-feature.md` — read before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).
