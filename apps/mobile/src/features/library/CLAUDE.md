# library — feature-local router

Single chip-filtered Library screen (`docs/superpowers/specs/2026-06-28-library-redesign-design.md`). Opens to Playlists; a persistent search + chip bar swaps one focused view at a time. `LibraryChip` is the active view: `'playlists' | 'tracks' | 'albums' | 'artists'`.

Layout:

- `ui/LibraryScreen.tsx` — orchestrator; owns `chip`, per-chip `sortByChip`, search, the track action sheet, and the loading/error/empty branches.
- `ui/LibraryChips.tsx`, `ui/SortControl.tsx`, `ui/LibraryNoResults.tsx`.
- `ui/PlaylistsGrid.tsx`, `ui/TracksList.tsx`, `ui/AlbumsGrid.tsx`, `ui/ArtistsGrid.tsx`, `ui/LibraryRow.tsx`.
- `ui/PlaylistDetailScreen.tsx` / `ui/PlaylistHero.tsx` — route `/library/playlist/[id]`.
- `ui/trackMenu.ts` — `buildTrackMenuItems`. `ui/sort.ts` — pure sorters + `*_SORT_OPTIONS`.
- `hooks/useLibraryHome.ts`, `hooks/usePlaylistMutations.ts`, `hooks/useLibrarySearch.ts`. `state.ts` — `_viewForState`.
- `__tests__/` — `LibraryScreen`, `sort`, `LibraryRow{,.retry}`, `useLibraryGrouping`, `library-to-discovery`, `formatFailureReason`, `useLibrarySearch`, `LibraryNoResults`, `useRetryAcquisition`.

Dependencies: `@shared/ui` (plus `primitives/{ActionSheet,Artwork,SearchBar}` directly — native deps, structure audit F2), `@shared/lib/{format,derive-library-groups,detail-handoff,query-keys}`, `@shared/playback`.

## Rules

- The noun is **Track**, never "Song" — chip and list vocabulary included.
- Import cache keys from `libraryKeys` / `playlistKeys` in `@shared/lib/query-keys`; never retype a key literal.
- Route every playlist write through `usePlaylistMutations` — it owns the optimistic-patch/rollback/alert/invalidate policy; screens keep only UI state.
- Assemble the track context menu only in `ui/trackMenu.ts`.
- Keep the sorters in `ui/sort.ts` pure.
- Derive albums and artists client-side; never add a backend grouping endpoint.
- Never show the empty-library CTA when a search has merely filtered the view to zero.
- Navigate to detail through `useLibraryNavigation` + the handoff seam.

Why each rule exists, and what is deliberately deferred: `okf/mobile/library-feature.md` — read before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).
