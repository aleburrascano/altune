# Playlist & Queue Polish — Design Spec

**Date:** 2026-06-19
**Status:** Approved
**Scope:** Frontend (apps/mobile) + playback architecture

## 1. Playlist Detail Screen — Immersive Gradient Redesign

### Hero area
- Gradient background derived from artwork colors, fading from the dominant color at top into canvas (`#121214`) at ~70% height.
- Large floating artwork (160px) centered on the gradient with `box-shadow: 0 12px 40px rgba(0,0,0,0.5)`.
- Playlist name below artwork, `displayL` variant (tap to rename — same behavior as current).
- Track count + total duration: `"24 tracks · 1h 42m"`, `caption` variant, `textSecondary` tone.

### Play controls
- Two pill buttons below the metadata, max-width 240px, centered:
  - **Play** — accent background, `"▶ Play"`, starts playlist from track 1 top-down, loads into TrackPlayer native queue.
  - **Shuffle** — `surface1` background with `border` color border, `"⇅ Shuffle"`, starts at random track with shuffle on, loads into native queue.

### Header
- **Back button** — 32px circle, `surface1` background, chevron left icon. Top-left.
- **Overflow button** — 32px circle, `surface1` background, three-dot icon. Top-right.
- No exposed Delete button anywhere on the screen.

### Overflow menu — Context Dropdown
- Small floating card anchored below the three-dot button, top-right.
- Background `surface2`, 1px `border`, `radius.lg`, `box-shadow: 0 8px 32px rgba(0,0,0,0.6)`.
- Two items:
  - **Rename Playlist** — opens inline rename on the playlist name.
  - **Delete Playlist** — red text (`danger` color), triggers confirmation Alert.
- Dismiss on tap outside or on item tap.
- New component: `ContextMenu` in `shared/ui/primitives/`.

### Track rows
- Artwork thumbnail (40px, `radius.sm`), title, artist, duration.
- Now-playing indicator: title in `accent` color, blue overlay on artwork thumbnail with play icon.
- Track number on left (optional, `textTertiary`).
- Long-press → ActionSheet with "View Details" / "Remove from Playlist".
- `FlatList` with `ListHeaderComponent` for the hero.

### Empty state
- Centered text: "No tracks yet" / "Use the menu on any track to add it here" (keep current).

## 2. Queue Screen — Now Playing + Up Next

### Header
- Chevron-down button (dismiss), left.
- "Up Next" title centered, source label below: `"Playing from Chill Vibes"` / `"Playing from Library"`.
- "Clear" button, right, `danger` color. Tapping triggers confirmation Alert, then `clearQueue()`.

### Now Playing row
- Full-width accent left border (3px, `accent` color).
- Subtle gradient background: `linear-gradient(90deg, rgba(accent, 0.08), transparent)`.
- Artwork (48px) with play badge (16px accent circle with play icon, bottom-right corner).
- "NOW PLAYING" label in `accent` color, uppercase, `caption` size, `letter-spacing: 0.5`.
- Track title (`body` variant, `textPrimary`), artist (`caption`, `textSecondary`), duration.
- Not draggable, not swipeable.

### Up Next section
- Section label: `"Up Next · 22 tracks"`, uppercase, `textSecondary`, `caption` size.
- Track rows with:
  - Drag handle (`⠿⠿` grip dots, `textTertiary`) on the left — long-press to reorder.
  - Artwork (40px), title, artist, duration.
  - Swipe left → red "Remove" action (using `ReanimatedSwipeable` from react-native-gesture-handler).
- Tap any track → `skipToIndex()` and start playing.
- `FlatList` with `ListHeaderComponent` for the now-playing row.

### Drag-to-reorder
- Use `react-native-gesture-handler` drag gesture on the handle.
- On drop, reorder the `playOrder` array in the queue store. New action: `reorderQueue(fromIndex, toIndex)`.

## 3. Playlist Cover — Adaptive Grid

Update `PlaylistCover` component to adapt layout based on artwork count:

| Artworks | Layout |
|----------|--------|
| 0 | Gradient background (`surface2`) + music note icon (♪), 30% opacity |
| 1 | Single image fills entire cover |
| 2 | Vertical 50/50 split |
| 3 | 2×2 grid, 4th cell = gradient + music note icon |
| 4+ | 2×2 grid (current behavior, unchanged) |

All layouts use `radius.sm` (8px) outer border radius, `overflow: 'hidden'`.

## 4. Reactivity Fixes

### Pull-to-refresh
- `LibraryScreen.onRefresh` must invalidate BOTH `['library-home']` AND `['playlists']` queries.
- Currently only refetches tracks via `useLibraryHome`.

### Mutation timing
- `AddToPlaylistSheet.addMut.onSettled`: invalidate queries and `await` the invalidation before closing the sheet. Replace `setTimeout(onClose, 600)` with waiting for the refetch to complete.
- `AddToPlaylistSheet.createMut.onSettled`: same — await invalidation, then close.
- `PlaylistDetailScreen.removeMut.onSettled`: await invalidation before allowing next interaction.

### Optimistic updates (already partially done)
- `addMut`: optimistically increment `track_count` on the playlist in cache (done).
- Cover artwork URLs: can only come from server — rely on invalidation + refetch.

### Error handler audit
- Review all mutations in playlist-related files for false-positive error detection.
- The queue-state 500 spam is fixed (migration + try/catch).
- Verify that `ApiError` messages are correctly interpreted — ensure no error is shown when the server returns 2xx.

## 5. Background Playback — Native Queue Migration

### Current architecture
- Zustand `queueStore` holds the queue state (tracks, playOrder, currentIndex).
- `useQueuePlayback.playFromList()` calls `loadQueue()` then `play(track)` on each transition.
- `play(track)` calls `TrackPlayer.load()` for one track at a time.
- JS thread must wake up to load the next track → ~5s gap in background.

### Target architecture
- When `playFromList()` is called, use `TrackPlayer.reset()` then `TrackPlayer.add(allTracks)` to load the full playlist into the native queue.
- `TrackPlayer.skip(startIndex)` to start at the correct track.
- Shuffle: use `TrackPlayer.setQueue()` with the shuffled order, or manage via `playOrder` mapping.
- Repeat: `TrackPlayer.setRepeatMode()` (native repeat).
- Zustand store syncs FROM TrackPlayer events (`Event.PlaybackActiveTrackChanged`, `Event.PlaybackState`) instead of driving it.
- `skipToNext` / `skipToPrevious` → `TrackPlayer.skipToNext()` / `TrackPlayer.skipToPrevious()` (native, no JS gap).

### What stays in Zustand
- `source` (where the queue came from — playlist, library, search).
- `shuffled` flag (to toggle shuffle UI state).
- `repeatMode` (synced with TrackPlayer's native repeat mode).
- `currentIndex` (synced from `Event.PlaybackActiveTrackChanged`).

### What moves to TrackPlayer
- Track list and play order (the native queue IS the source of truth).
- Track transitions (native, gapless).
- Skip next/previous (native).
- Repeat mode enforcement (native).

### Migration approach
- Update `useQueuePlayback` to use `TrackPlayer.add()` / `TrackPlayer.skip()` instead of `play(track)`.
- Add event listeners in the playback provider to sync Zustand from TrackPlayer state.
- Update `useQueueResume` to restore into TrackPlayer's native queue.
- Keep `queueStore` as the UI read-model, but it's no longer the driver.

## 6. Non-goals (explicitly excluded)

- Drag-to-reorder on playlist detail track list (future spec — needs `reorderPlaylistTracks` API wiring).
- Light mode support.
- Playlist sharing.
- Color extraction from artwork (use a fixed gradient for v1; dynamic extraction is a future enhancement).
