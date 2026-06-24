# view-library — feature-local context

Sectioned library home (revamp-library-v1 spec). Replaces the original flat FlatList with a scrollable dashboard: Playlists (placeholder), Recently Added tracks, horizontal Album carousel, horizontal Artist carousel. Each section expands in-place with sort chips (Recent / A–Z / Year). Profile avatar in header opens a bottom sheet with email + sign out.

## Key terms

- **Track** — single audio recording with title + artist (+ optional album, duration, artwork, year, genre, track_number, album_artist, isrc, audio_ref). Mirror of `services/api/src/altune/domain/catalog/Track`. Defined globally in `docs/ubiquitous-language.md`.
- **AlbumGroup** / **ArtistGroup** — client-side groupings derived from library tracks by `album + album_artist` or `artist`. Not domain types — UI read-side concerns only.
- **`apiBase`** — the resolved API URL the bundle was built with. Comes from `EXPO_PUBLIC_API_URL`.

## Layout

- `ui/LibraryScreen.tsx` — sectioned home with expand/collapse state machine. Handles loading/error/empty states. Wires navigation to Detail screen for tracks, albums, artists.
- `ui/LibraryRow.tsx` — rich track row: `Artwork` (48px) + title + artist · album + duration (M:SS).
- `ui/AlbumCarousel.tsx` — horizontal FlatList of album covers with title + artist.
- `ui/ArtistCarousel.tsx` — horizontal FlatList of circular artist avatars.
- `ui/PlaylistPlaceholder.tsx` — dashed-border "Coming Soon" card.
- `ui/ProfileSheet.tsx` — Modal bottom sheet with avatar, email, sign out.
- `hooks/useLibraryHome.ts` — fetches all tracks (limit 2000) via `useQuery`, derives recent + groups.
- `hooks/useLibraryGrouping.ts` — memoized client-side album/artist derivation. Pure functions `deriveAlbums` / `deriveArtists` exported for testing.
- `ui/PlaylistCarousel.tsx` — horizontal FlatList of playlist cards with collage cover + "Create" button.
- `ui/PlaylistCover.tsx` — 2x2 collage from up to 4 artwork URLs.
- `ui/PlaylistDetailScreen.tsx` — playlist detail: cover, rename (tap name), delete, tracklist with per-track remove. Route: `/library/playlist/[id]`.
- `ui/CreatePlaylistModal.tsx` — modal with text input for naming a new playlist.
- `hooks/useLibrary.ts` — pure pagination helpers (`_nextOffsetFromPage`, `_flattenPages`) + the `LibraryState` shape. The `useInfiniteQuery` hook was removed (no live caller); rebuild fresh on these helpers when the expanded Songs view ships.
- `ui/library-to-discovery.ts` — pure `albumToDiscoveryResult` / `artistToDiscoveryResult` mappers (siblings of shared `trackToDiscoveryResult`); feature-local until a 2nd consumer.
- `state.ts` — `_viewForState` pure helper (loading > error > empty > list).

## Patterns

- **Client-side grouping** (AC#11): albums/artists derived from `TrackResponse[]` keyed by normalized album+artist / artist. No backend endpoint needed.
- **In-place expand**: state variable `expanded: 'recent' | 'albums' | 'artists' | null` swaps the ScrollView sections for a full-screen FlatList with sort chips. "Collapse" returns to the sectioned home.
- **Profile via Modal**: `ProfileSheet` uses RN `Modal` (portal-based) — no external dependency.
- **Navigation to Detail**: library tracks/albums/artists build a `DiscoveryResult` and use the existing detail handoff pattern. Navigates to `/library/detail` (within the library tab's stack).

## Dependencies

- `@shared/ui` — Artwork, Row, Chip, Text, Button, Screen, Skeleton, spacing, useTheme.
- `@shared/lib/format` — `formatDuration` (promoted from detail/extras.ts).
- `@shared/lib/detail-handoff` — the discover↔detail seam.
- `features/auth/hooks/useSession` — email for profile avatar.
- `features/auth/hooks/useSignOut` — sign-out from profile sheet.

## Test files

- `__tests__/LibraryRow.test.tsx` — row rendering.
- `__tests__/LibraryScreen.test.ts` — screen state machine (may need updating for sectioned home).
- `__tests__/useLibrary.test.ts` — pagination helpers.
