# library — feature-local context

Single chip-filtered Library screen (library-redesign, `docs/superpowers/specs/2026-06-28-library-redesign-design.md`). Opens to **Playlists**; a persistent search + chip bar swaps one focused view at a time — no stacked overview. Model: **spine + lenses** — Songs and Playlists are what the user owns (primary); Albums and Artists are derived groupings of saved songs (lenses).

## Key terms

- **Track** — single audio recording with title + artist (+ optional album, duration, artwork, year, genre, track_number, album_artist, isrc, audio_ref). Defined globally in `docs/ubiquitous-language.md`.
- **AlbumGroup** / **ArtistGroup** — client-side groupings derived from library tracks (`@shared/lib/derive-library-groups`). Not domain types — UI read-side lenses.
- **LibraryChip** — the active view: `'playlists' | 'songs' | 'albums' | 'artists'` (`ui/LibraryChips.tsx`).

## Layout

- `ui/LibraryScreen.tsx` — orchestrator. Owns `chip`, per-chip `sortByChip`, search, and the track action sheet; renders header + search + chip bar + sort control + the active view. Handles loading/error/empty (`_viewForState`); empty-library (no tracks **and** no playlists) shows the Discover CTA.
- `ui/LibraryChips.tsx` — the type chip bar (`Playlists · Songs · Albums · Artists`), horizontally scrollable.
- `ui/SortControl.tsx` — count + tappable sort label opening an `ActionSheet` of the view's sort options.
- `ui/PlaylistsGrid.tsx` — 2-col grid of `PlaylistCover` collages + a "New Playlist" tile (cover size from `useWindowDimensions`).
- `ui/SongsList.tsx` — `LibraryRow` list (the spine). Play context `{ kind: 'library' }`.
- `ui/AlbumsGrid.tsx` — 2-col album cover grid (lens).
- `ui/ArtistsGrid.tsx` — 3-col circular artist grid (lens).
- `ui/LibraryRow.tsx` — rich track row: `Artwork` (48px) + title + artist · album + duration; inline pending/failed + retry.
- `ui/LibraryNoResults.tsx` — shown when the search filters the active view to zero: names the query, states the library is intact, offers one-tap clear. A filtered-out library must never look like a missing one (the 2026-07-14 "entire library is missing" report was a persisted search filter).
- `ui/PlaylistDetailScreen.tsx` / `ui/PlaylistHero.tsx` — playlist detail (centered hero), unchanged. Route `/library/playlist/[id]`.
- `ui/sort.ts` — pure `sortPlaylists` / `sortTracks` / `sortAlbums` / `sortArtists` + `*_SORT_OPTIONS`. `SortKey = 'recent' | 'az' | 'year'`.
- `hooks/useLibraryHome.ts` — fetches all tracks (limit 2000, polls while any track is pending) + derives album/artist groups.
- `hooks/useLibrarySearch.ts` — debounced search: `filter(tracks)` for songs, `matches(text)` predicate for album/artist/playlist names.
- `state.ts` — `_viewForState` pure helper (loading > error > empty > list).

## Patterns

- **One screen, chip-filtered**: the chips are the navigation. Selecting a chip swaps only the content area; search and per-chip sort persist. Replaces the old stacked home + separate `all-tracks`/`all-albums`/`all-artists` routes.
- **Client-side grouping**: albums/artists derived from `TrackResponse[]`. No backend endpoint.
- **Recently Added = Songs sorted Recent** — not a separate section.
- **Navigation to Detail**: tracks/albums/artists build a `DiscoveryResult` and navigate to `/library/detail` via `useLibraryNavigation` + the detail handoff seam.

## Deferred

- **Favorites / Liked** — not built; saving is already the deliberate act. A pinned slot can be added later.
- **Jump back in / recently played** — a Home concern, reserved for a future Home tab; not in Library.

## Dependencies

- `@shared/ui` — Screen, SearchBar, Chip, Artwork, Row, Text, Button, Skeleton, spacing, radius, useTheme; `primitives/ActionSheet`.
- `@shared/lib/format` — `formatDuration`. `@shared/lib/derive-library-groups` — grouping. `@shared/lib/detail-handoff` — discover↔detail seam.
- `@shared/playback` — queue/playback for in-library play.

## Test files

- `__tests__/LibraryScreen.test.ts` — `_viewForState` state machine.
- `__tests__/sort.test.ts` — sort helpers (playlists/tracks/albums/artists).
- `__tests__/LibraryRow.test.tsx` / `LibraryRow.retry.test.tsx` — row rendering + retry.
- `__tests__/useLibraryGrouping.test.ts`, `library-to-discovery.test.ts`, `formatFailureReason.test.ts`, `useLibrary.test.ts`.
- `__tests__/useLibrarySearch.test.ts` — debounce/commit + the stale-committed-query regression (deleting to 1 char must lift the filter).
- `__tests__/LibraryNoResults.test.tsx` — no-results view names the query and clears on tap.
