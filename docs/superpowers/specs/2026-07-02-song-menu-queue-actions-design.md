# Song Menu: Add to Queue / Play Next + Anchored Dropdown Restyle

**Date:** 2026-07-02
**Status:** Approved design, pending implementation plan

## Problem

The 3-dot (⋮) menu on a song row opens `ActionSheet` — a bottom sheet with a
large title/subtitle, hairline-divided rows, and a separate "Cancel" block. It
looks dated and inconsistent with the clean floating dropdown (`ContextMenu`)
used by the 3-dot menu in the playlist detail screen header.

Separately, there is no way to add a song to the play queue. The queue store has
no append/insert action and neither playback provider exposes one.

## Goals

1. Restyle the song row menu to match the playlist header menu: an anchored
   floating dropdown card, reusing the existing `ContextMenu` primitive.
2. Add two queue actions to the song menu: **Play Next** (insert after the
   current track) and **Add to Queue** (append to the end).

## Non-Goals

- No toast/confirmation feedback after enqueuing (no existing toast primitive;
  can follow up later).
- No drag-to-reorder or queue-management UI.
- No changes to the playlist header menu (already uses `ContextMenu`).
- `ActionSheet` primitive is **not** removed — it is still used by `SortControl`
  and `DownloadsSheet`.

## Design

### 1. Anchored dropdown for song rows

Replace the song-row `ActionSheet` with the existing `ContextMenu` primitive,
anchored to the tapped ⋮ button.

- **`LibraryRow`** (`features/library/ui/LibraryRow.tsx`): give the ⋮ `Pressable`
  a ref; on press, call `measureInWindow` and pass the button's screen rect up.
  `onMore` changes from `() => void` to `(anchor) => void`, where `anchor`
  carries the button's top Y, bottom Y, and right offset.
- **`SongsList`** (`features/library/ui/SongsList.tsx`): forward the anchor
  through its `onMore` prop.
- **`LibraryScreen`** and **`PlaylistDetailScreen`**: replace the per-track
  `ActionSheet` with a `ContextMenu`. State becomes `{ track, anchor }` instead
  of just `actionTrack`.
- **`ContextMenu`** (`shared/ui/primitives/ContextMenu.tsx`): teach it to flip
  **upward** when the anchor is too close to the bottom of the screen (menu would
  otherwise overflow). It estimates its own height from item count, then uses
  `useWindowDimensions` + safe-area insets to decide open-down (from the button
  bottom) vs open-up (from the button top). The up/down decision is extracted as
  a **pure helper** so it can be unit-tested.

The playlist header menu keeps using `ContextMenu` unchanged, so both surfaces
stay consistent by sharing the primitive.

### 2. Queue actions — store + native lockstep

The codebase enforces a hard rule (`queueStore.ts` AIDEV-WARNING): any JS queue
mutation must mirror the native `TrackPlayer` queue in lockstep, because native
index == play-order position == store `currentIndex`.

- **`queueStore`** — two new actions:
  - `enqueue(track)`: append. Push `track` onto `tracks`; push its new index
    onto the end of `playOrder`.
  - `playNext(track)`: push `track` onto `tracks`; splice its new index into
    `playOrder` at `currentIndex + 1`.
- **`loadNativeTrack.ts`** — two exported helpers reusing the existing private
  `toNativeTrack` + auth-header logic:
  - `appendNativeTrack(track)` → `TrackPlayer.add(nativeTrack)` (appends at end).
  - `insertNativeTrackNext(track, pos)` → `TrackPlayer.add(nativeTrack, pos)`
    (`add` with `insertBeforeIndex`).
- **`trackPlayerProvider`** — expose `appendToQueue` and `insertNext` controls
  that call the helpers above.
- **`expoGoPlaybackProvider`** — no-op stubs for both (parity).
- **`types.ts`** — add `appendToQueue(track)` and `insertNext(track, pos)` to
  `PlaybackControls`.
- **`useQueuePlayback`** — orchestrate store + native together (same shape as the
  existing `toggleShuffle`):
  - `addToQueue(track)`: if the queue is **empty** (`orderedQueueTracks` length
    0), fall back to `playTrack(track)` — nothing to queue behind, so just play
    it. Otherwise `enqueue` in the store and native-append.
  - `playNext(track)`: read `insertPos = currentIndex + 1` from the store,
    mutate the store via `playNext`, then native `insertNext(track, insertPos)`.

### 3. Menu items

Track is converted at the call site via `toPlaybackTrack`.

For a **ready** track, menu order:
1. Play Next
2. Add to Queue
3. Add to Playlist *(library screen only)*
4. View Details
5. Remove from Library / Remove from Playlist *(danger)*

For a **pending** or **failed** track, omit *Play Next* and *Add to Queue* (no
playable audio yet).

## Testing

- **`queueStore`** unit tests: `enqueue` and `playNext` play-order correctness
  with shuffle **off** and **on**, and the empty-queue path.
- **Native-call tests** (mirroring `reorderUpcomingNative.test.ts`): assert
  `TrackPlayer.add` is called with the right native track and, for `playNext`,
  the correct `insertBeforeIndex`.
- **Pure unit test** for the `ContextMenu` flip-direction helper (anchor near
  top → open down; anchor near bottom → open up).

## Risks / Tradeoffs

- An anchored dropdown that flips up/down is inherently more finicky than a
  bottom sheet, especially mid-scroll. Chosen deliberately for visual
  consistency; mitigated by the flip logic and the pure, tested helper.
- No new dependencies.
