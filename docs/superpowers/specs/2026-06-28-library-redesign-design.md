# Library redesign — design

**Date:** 2026-06-28
**Status:** Accepted
**Scope:** `apps/mobile/src/features/library` + its routes under `apps/mobile/src/app/(tabs)/library`

## Problem

The Library home is a single vertically-stacked dashboard: Playlists carousel → Recently Added rows → Albums carousel → Artists carousel, with three separate "See all" destinations (`all-tracks`, `all-albums`, `all-artists`). Two issues:

1. **Density / scrolling.** The home tries to show a slice of every type at once, so it stacks four sections and scrolls a lot.
2. **Flat hierarchy.** Playlists (the only thing the user curates) sit at the same visual weight as Albums/Artists, which are merely client-side groupings of saved tracks.

## Model: spine + lenses

The Library contains only two things the user *owns*:

- **Songs** — the saved tracks; the spine everything is built from.
- **Playlists** — user-created, curated collections.

**Albums** and **Artists** are **lenses** — derived by grouping the user's saved songs (`deriveAlbums` / `deriveArtists`), not separately-saved entities. They are *views over the songs*, not peer collections.

This model sets the hierarchy: Songs and Playlists are primary; Albums and Artists are secondary slices.

## Design: one screen, four chip-filtered views

The Library is **one screen** with a persistent chrome and a swappable content area. No "All" overview, no stacked sections.

**Persistent chrome (always visible):**
- Header: `Library` (`displayL`).
- **Search** bar (filters the *active* view).
- **Chip bar:** `Playlists · Songs · Albums · Artists`. Selecting a chip swaps only the content area.

**Content area:** exactly one type at a time, each with a **count + sort control** row.

| View | Default sort | Sort options | Layout |
|------|--------------|--------------|--------|
| **Playlists** (landing) | Recent | Recent, A–Z | 2-col grid of collage covers + a "New Playlist" tile |
| **Songs** (spine) | Recent | Recent, A–Z, Year | vertical list (current `LibraryRow`) |
| **Albums** (lens) | A–Z | A–Z, Recent, Year | 2-col grid of covers (title + artist) |
| **Artists** (lens) | A–Z | A–Z, Recent | 3-col grid of circular avatars |

**Default landing:** the Library opens to the **Playlists** chip.

**"Recently Added" is removed as a concept** — it is simply the Songs view with its default `Recent` sort. The top of Songs *is* recently added.

**Search** filters the currently-active view (e.g. on Songs it filters tracks; on Albums it filters album groups). Search state persists across chip switches; an empty query shows the full view.

**Sort** is a tappable control (`Recent ⌄`) that opens a small menu of the options for the active view. Sort selection is per-view.

## Playlist detail — unchanged

The existing centered-hero `PlaylistDetailScreen` (160px cover + name + `N tracks · Xm` + Play/Shuffle pills + cobalt gradient + `LibraryRow` tracklist, rename via tap, delete via overflow menu, per-track remove via action sheet) is kept as-is. No work.

## Routing changes

The chip states replace the separate "See all" routes:

- **Removed routes:** `app/(tabs)/library/all-tracks.tsx`, `all-albums.tsx`, `all-artists.tsx` — their content becomes the Songs / Albums / Artists chip states on the index screen.
- **Kept routes:** `index.tsx` (now the chip-filtered screen), `playlist/[id].tsx`, `detail.tsx`, `_layout.tsx`.
- **Screens removed:** `AllTracksScreen.tsx`, `AllAlbumsScreen.tsx`, `AllArtistsScreen.tsx` (their list/grid logic moves into the per-type view components).

## Components

New / changed under `features/library/ui/`:

- **`LibraryScreen.tsx`** — owns chip state (`'playlists' | 'songs' | 'albums' | 'artists'`), search query, and per-view sort; renders header + search + chip bar + the active view. Handles loading/error/empty.
- **`LibraryChips.tsx`** — the type chip bar (reuses `@shared/ui` `Chip`).
- **`SortControl.tsx`** — count + tappable sort label opening an `ActionSheet`/`ContextMenu` of options.
- **`PlaylistsGrid.tsx`** — 2-col grid + New Playlist tile (replaces `PlaylistCarousel` on the home).
- **`SongsList.tsx`** — `LibraryRow` list (the `all-tracks` logic).
- **`AlbumsGrid.tsx`** — 2-col album grid (replaces `AlbumCarousel`).
- **`ArtistsGrid.tsx`** — 3-col circular artist grid (replaces `ArtistCarousel`).

Reused as-is: `LibraryRow`, `PlaylistCover`, `LibraryHeader`, the playlist sheets/modals, `useLibraryNavigation`, `library-to-discovery`.

Hooks: `useLibraryHome` (already fetches all tracks + derives recent/albums/artists), `useLibraryGrouping` (`deriveAlbums`/`deriveArtists`), `useLibrarySearch`, `usePlaylistActions`, `sort.ts` — adapted to feed the per-view components. Carousels (`AlbumCarousel`, `ArtistCarousel`, `PlaylistCarousel`, `SectionHeader`) are removed once no caller remains.

## Out of scope / deferred

- **Favorites / Liked** — not built; saving to library is already the deliberate act, so it would duplicate "saved". A slot can be added later (e.g. a pinned Playlists entry) if a like action ships.
- **"Jump back in" / recently played** — a home/landing concern, reserved for a possible future Home tab; explicitly not in Library.
- **Acquisition status surface** — pending/failed status stays inline on `LibraryRow` (with its retry affordance) exactly as today; no dedicated "Needs attention" view.

## Testing

- **State machine:** chip selection swaps the active view; default is `playlists`.
- **Search:** filters the active view; persists across chip switches; empty query restores full view.
- **Sort:** per-view sort options; default sorts (`Recent` for Playlists/Songs, `A–Z` for Albums/Artists); selection re-orders.
- **Grouping:** existing `deriveAlbums`/`deriveArtists` + `sort.ts` unit tests remain green; extend for the new sort options where added.
- **States:** loading (skeletons), error (retry), empty library, and empty *within a view* (e.g. no playlists yet → New Playlist tile still shown).
- Update `LibraryScreen.test.ts` for the chip-driven structure; keep `LibraryRow`, `useLibrary`, `useLibraryGrouping`, `sort` tests.

## Success criteria

1. Library opens to Playlists; chips switch among Playlists/Songs/Albums/Artists with one focused view on screen at a time.
2. Search and sort work per-view and persist appropriately.
3. The three `all-*` routes/screens are gone; their function is reachable via chips.
4. Playlist detail is unchanged and still passes its tests.
5. Full mobile test suite green; typecheck clean.
