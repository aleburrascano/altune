import { create } from 'zustand';

import type { PlaybackTrack, QueueSource, RepeatMode } from './types';

interface QueueState {
  tracks: readonly PlaybackTrack[];
  playOrder: readonly number[];
  currentIndex: number;
  repeatMode: RepeatMode;
  shuffled: boolean;
  source: QueueSource | null;
  // Saved playback offset (ms) restored on relaunch, shown on the scrubber before
  // the native player loads and reports live progress. 0 in every fresh queue;
  // set only by the resume flow. Once native progress goes live (> 0) the provider
  // ignores it, so it never fights real playback.
  resumePositionMs: number;
}

interface QueueActions {
  loadQueue: (tracks: readonly PlaybackTrack[], startIndex: number, source: QueueSource | null) => void;
  restoreQueue: (
    tracks: readonly PlaybackTrack[],
    playOrder: readonly number[],
    currentIndex: number,
    source: QueueSource | null,
    shuffled: boolean,
  ) => void;
  enqueue: (track: PlaybackTrack) => void;
  playNext: (track: PlaybackTrack) => void;
  skipToNext: () => PlaybackTrack | null;
  skipToPrevious: () => PlaybackTrack | null;
  skipToIndex: (index: number) => PlaybackTrack | null;
  syncCurrentIndex: (index: number) => void;
  setResumePosition: (positionMs: number) => void;
  toggleShuffle: () => void;
  setShuffled: (shuffled: boolean) => void;
  cycleRepeatMode: () => void;
  setRepeatMode: (mode: RepeatMode) => void;
  reorderQueue: (fromIndex: number, toIndex: number) => void;
  removeFromQueue: (index: number) => void;
  clearQueue: () => void;
  currentTrack: () => PlaybackTrack | null;
  hasNext: () => boolean;
  hasPrevious: () => boolean;
}

export type QueueStore = QueueState & QueueActions;

const INITIAL: QueueState = {
  tracks: [],
  playOrder: [],
  currentIndex: -1,
  repeatMode: 'off',
  shuffled: false,
  source: null,
  resumePositionMs: 0,
};

function identityOrder(length: number): number[] {
  return Array.from({ length }, (_, i) => i);
}

// Shuffle only the tail after `keepThrough` (inclusive), leaving the head — the
// already-played history and the current track — untouched. Keeping the current
// track's position fixed is what lets the native queue reorder around the
// active track without ever touching it (seamless, no re-buffer).
function shuffleTail(order: readonly number[], keepThrough: number): number[] {
  const head = order.slice(0, keepThrough + 1);
  const tail = order.slice(keepThrough + 1);
  for (let i = tail.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    const tmp = tail[i]!;
    tail[i] = tail[j]!;
    tail[j] = tmp;
  }
  return [...head, ...tail];
}

function trackAt(tracks: readonly PlaybackTrack[], playOrder: readonly number[], index: number): PlaybackTrack | null {
  const trackIndex = playOrder[index];
  if (trackIndex == null) return null;
  return tracks[trackIndex] ?? null;
}

const REPEAT_CYCLE: Record<RepeatMode, RepeatMode> = {
  off: 'all',
  all: 'one',
  one: 'off',
};

export const useQueueStore = create<QueueStore>((set, get) => ({
  ...INITIAL,

  loadQueue: (tracks, startIndex, source) => {
    const order = identityOrder(tracks.length);
    set({ tracks, playOrder: order, currentIndex: startIndex, shuffled: false, source, resumePositionMs: 0 });
  },

  // Restore a snapshot with an EXPLICIT play-order permutation (unlike loadQueue,
  // which forces identity order). Resume uses this to rebuild the exact shuffled
  // sequence from the persisted natural + play orders, so tracks stays in natural
  // (album/playlist) order and un-shuffle returns to it. currentIndex is a
  // position in playOrder.
  restoreQueue: (tracks, playOrder, currentIndex, source, shuffled) => {
    const clampedIdx =
      playOrder.length === 0 ? -1 : Math.max(0, Math.min(currentIndex, playOrder.length - 1));
    set({ tracks, playOrder, currentIndex: clampedIdx, shuffled, source, resumePositionMs: 0 });
  },

  // AIDEV-WARNING: enqueue/playNext mutate tracks + playOrder, so any caller
  // MUST also add to the native queue in lockstep (TrackPlayer.add). The new
  // track's index is tracks.length (its slot in the appended `tracks`); native
  // queue position == play-order position, so append maps to add-at-end and
  // "play next" maps to add-at(currentIndex+1). See useQueuePlayback.
  enqueue: (track) => {
    const { tracks, playOrder } = get();
    set({ tracks: [...tracks, track], playOrder: [...playOrder, tracks.length] });
  },

  playNext: (track) => {
    const { tracks, playOrder, currentIndex } = get();
    const newTrackIndex = tracks.length;
    const insertAt = currentIndex + 1;
    const newOrder = [
      ...playOrder.slice(0, insertAt),
      newTrackIndex,
      ...playOrder.slice(insertAt),
    ];
    set({ tracks: [...tracks, track], playOrder: newOrder });
  },

  skipToNext: () => {
    const { tracks, playOrder, currentIndex, repeatMode } = get();
    if (tracks.length === 0) return null;
    if (repeatMode === 'one') return trackAt(tracks, playOrder, currentIndex);

    const next = currentIndex + 1;
    if (next < playOrder.length) {
      set({ currentIndex: next });
      return trackAt(tracks, playOrder, next);
    }
    if (repeatMode === 'all') {
      set({ currentIndex: 0 });
      return trackAt(tracks, playOrder, 0);
    }
    return null;
  },

  skipToPrevious: () => {
    const { tracks, playOrder, currentIndex, repeatMode } = get();
    if (tracks.length === 0) return null;

    const prev = currentIndex - 1;
    if (prev >= 0) {
      set({ currentIndex: prev });
      return trackAt(tracks, playOrder, prev);
    }
    if (repeatMode === 'all') {
      const last = playOrder.length - 1;
      set({ currentIndex: last });
      return trackAt(tracks, playOrder, last);
    }
    set({ currentIndex: 0 });
    return trackAt(tracks, playOrder, 0);
  },

  skipToIndex: (index) => {
    const { tracks, playOrder } = get();
    if (index < 0 || index >= playOrder.length) return null;
    set({ currentIndex: index });
    return trackAt(tracks, playOrder, index);
  },

  // Reconcile currentIndex with the native player after a native-driven
  // transition (auto-advance, lock-screen next/prev). The native queue is the
  // transition engine; this keeps the store — the UI read-model — in lockstep.
  // A no-op when already aligned, so it can't feed back into another native skip.
  syncCurrentIndex: (index) => {
    const { playOrder, currentIndex } = get();
    if (index < 0 || index >= playOrder.length) return;
    if (index === currentIndex) return;
    set({ currentIndex: index });
  },

  setResumePosition: (positionMs) => {
    set({ resumePositionMs: Math.max(0, positionMs) });
  },

  // Shuffle/unshuffle only the upcoming tracks; the current track and history
  // keep their positions so currentIndex is stable and the native player never
  // has to touch the playing track. Turning shuffle off restores the upcoming
  // tracks to their natural (ascending) order rather than rebuilding the whole
  // queue — history stays as played, which is what keeps the toggle seamless.
  toggleShuffle: () => {
    const { tracks, playOrder, currentIndex, shuffled } = get();
    if (tracks.length <= 1) return;

    if (shuffled) {
      const head = playOrder.slice(0, currentIndex + 1);
      const tail = [...playOrder.slice(currentIndex + 1)].sort((a, b) => a - b);
      set({ playOrder: [...head, ...tail], shuffled: false });
    } else {
      set({ playOrder: shuffleTail(playOrder, currentIndex), shuffled: true });
    }
  },

  // Set the shuffled flag WITHOUT reordering. Used by resume: the saved queue is
  // already persisted in play order, so the loaded order IS the shuffled order —
  // marking it shuffled (rather than calling toggleShuffle, which re-randomizes
  // the tail) preserves the exact order the user was listening to.
  setShuffled: (shuffled) => {
    set({ shuffled });
  },

  cycleRepeatMode: () => {
    set((s) => ({ repeatMode: REPEAT_CYCLE[s.repeatMode] }));
  },

  setRepeatMode: (mode) => {
    set({ repeatMode: mode });
  },

  // AIDEV-WARNING: reorderQueue mutates playOrder, so any caller MUST also
  // reorder the native queue (TrackPlayer.move) in lockstep. The store's
  // currentIndex follows native by position (syncCurrentIndex); a store-only
  // reorder desyncs the UI from audio — the same class of bug that store-only
  // shuffle caused. Currently unused (drag-to-reorder isn't wired up yet).
  reorderQueue: (fromIndex, toIndex) => {
    const { playOrder, currentIndex } = get();
    if (fromIndex === toIndex) return;
    if (fromIndex < 0 || fromIndex >= playOrder.length) return;
    if (toIndex < 0 || toIndex >= playOrder.length) return;
    const newOrder = [...playOrder];
    const [moved] = newOrder.splice(fromIndex, 1);
    newOrder.splice(toIndex, 0, moved!);
    let newCurrent = currentIndex;
    if (fromIndex === currentIndex) {
      newCurrent = toIndex;
    } else if (fromIndex < currentIndex && toIndex >= currentIndex) {
      newCurrent = currentIndex - 1;
    } else if (fromIndex > currentIndex && toIndex <= currentIndex) {
      newCurrent = currentIndex + 1;
    }
    set({ playOrder: newOrder, currentIndex: newCurrent });
  },

  removeFromQueue: (index) => {
    const { tracks, playOrder, currentIndex, shuffled } = get();
    if (index < 0 || index >= playOrder.length) return;
    const trackIdx = playOrder[index]!;
    const newTracks = tracks.filter((_, i) => i !== trackIdx);
    if (newTracks.length === 0) {
      set(INITIAL);
      return;
    }
    const newOrder = playOrder
      .filter((_, i) => i !== index)
      .map((i) => (i > trackIdx ? i - 1 : i));
    const newCurrent = index < currentIndex
      ? currentIndex - 1
      : index === currentIndex
        ? Math.min(currentIndex, newOrder.length - 1)
        : currentIndex;
    set({ tracks: newTracks, playOrder: newOrder, currentIndex: newCurrent, shuffled: shuffled && newTracks.length > 1 });
  },

  clearQueue: () => set(INITIAL),

  currentTrack: () => {
    const { tracks, playOrder, currentIndex } = get();
    if (tracks.length === 0 || currentIndex < 0) return null;
    return trackAt(tracks, playOrder, currentIndex);
  },

  hasNext: () => {
    const { playOrder, currentIndex, repeatMode } = get();
    if (playOrder.length === 0) return false;
    if (repeatMode === 'all' || repeatMode === 'one') return true;
    return currentIndex < playOrder.length - 1;
  },

  hasPrevious: () => {
    const { playOrder, currentIndex, repeatMode } = get();
    if (playOrder.length === 0) return false;
    if (repeatMode === 'all') return true;
    return currentIndex > 0;
  },
}));

// The queue flattened into play-order — the exact sequence the native player
// loads. Native queue index == play-order position == store currentIndex, so
// transitions and removals map 1:1 between the store and TrackPlayer.
export function orderedQueueTracks(state: {
  tracks: readonly PlaybackTrack[];
  playOrder: readonly number[];
}): PlaybackTrack[] {
  return state.playOrder
    .map((i) => state.tracks[i])
    .filter((t): t is PlaybackTrack => t != null);
}
