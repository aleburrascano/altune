# library — feature-local router

Single chip-filtered Library screen (`docs/superpowers/specs/2026-06-28-library-redesign-design.md`). Opens to Playlists; a persistent search + chip bar swaps one focused view at a time. `LibraryChip` is the active view: `'playlists' | 'tracks' | 'albums' | 'artists'`.

Layout:

- `ui/LibraryScreen.tsx` — orchestrator; owns `chip`, per-chip `sortByChip`, search, the track action sheet, and the loading/error/empty branches.
- `ui/LibraryChips.tsx`, `ui/SortControl.tsx`, `ui/LibraryNoResults.tsx`.
- `ui/PlaylistsGrid.tsx`, `ui/TracksList.tsx`, `ui/AlbumsGrid.tsx`, `ui/ArtistsGrid.tsx`, `ui/LibraryRow.tsx`.
- `ui/PlaylistDetailScreen.tsx` / `ui/PlaylistHero.tsx` — route `/library/playlist/[id]`.
- `ui/trackMenu.ts` — `buildTrackMenuItems`. `ui/sort.ts` — the `*_SORT_OPTIONS` label lists; the sort keys are wire values the server applies.
- `hooks/useLibraryHome.ts` — `useLibraryTracks` (infinite), `useLibraryAlbums`, `useLibraryArtists`, one query per chip. `hooks/usePlaylistMutations.ts`, `hooks/useLibrarySearch.ts` (debounce only), `hooks/useRetryAcquisition.ts` (failed tracks), `hooks/useReacquireTrack.ts` (replace the audio of a ready track). `state.ts` — `_viewForState`.
- `__tests__/` — `LibraryScreen`, `LibraryRow{,.retry,.liveness}`, `library-to-discovery`, `useLibrarySearch`, `LibraryNoResults`, `useRetryAcquisition`, `gridColumns`.

Dependencies: `@shared/ui` (plus `primitives/{ActionSheet,Artwork,SearchBar}` directly — native deps, structure audit F2), `@shared/api-client/library`, `@shared/lib/{format,detail-handoff,query-keys}`, `@shared/playback`.

## Rules

- The noun is **Track**, never "Song" — chip and list vocabulary included.
- Re-acquire patches the cache on success only; a playing track must never be shown pending before the server accepts.
- A failed re-acquire restores the track to ready — it never renders a playable track as broken.
- Keep "Download" meaning offline pinning; audio replacement is "Re-acquire".
- Import cache keys from `libraryKeys` / `playlistKeys` in `@shared/lib/query-keys`; never retype a key literal.
- Route every playlist write through `usePlaylistMutations` — it owns the optimistic-patch/rollback/alert/invalidate policy; screens keep only UI state.
- Assemble the track context menu only in `ui/trackMenu.ts`.
- Send `q` and `sort` to the server; never filter or sort a library list in JS.
- Read albums and artists from `/v1/library/*`; never regroup a track list on the device.
- Keep each chip on its own query, enabled only while that chip is active.
- Never show the empty-library CTA when a search has merely filtered the view to zero.
- Navigate to detail through `useLibraryNavigation` + the handoff seam.

Why each rule exists, and what is deliberately deferred: `okf/mobile/library-feature.md` — read before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).
