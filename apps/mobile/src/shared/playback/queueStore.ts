import { create } from 'zustand';

import { trackKey } from './trackKey';
import type { PlaybackTrack, QueueSource, RepeatMode } from './types';

interface QueueState {
  tracks: readonly PlaybackTrack[];
  playOrder: readonly number[];
  currentIndex: number;
  repeatMode: RepeatMode;
  shuffled: boolean;
  source: QueueSource | null;
  resumePositionMs: number;
  generation: number;
}

interface QueueActions {
  loadQueue: (
    tracks: readonly PlaybackTrack[],
    startIndex: number,
    source: QueueSource | null,
  ) => void;
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
  syncCurrentIndex: (index: number, key?: string) => void;
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
  generation: 0,
};

function identityOrder(length: number): number[] {
  return Array.from({ length }, (_, i) => i);
}

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

function trackAt(
  tracks: readonly PlaybackTrack[],
  playOrder: readonly number[],
  index: number,
): PlaybackTrack | null {
  const trackIndex = playOrder[index];
  if (trackIndex == null) return null;
  return tracks[trackIndex] ?? null;
}

function nearestKeyPosition(
  ordered: readonly (PlaybackTrack | null)[],
  key: string,
  preferred: number,
): number | null {
  const hits = ordered.flatMap((t, i) => (t && trackKey(t) === key ? [i] : []));
  if (hits.length === 0) return null;
  return hits.reduce((a, b) => (Math.abs(b - preferred) < Math.abs(a - preferred) ? b : a));
}

function resolveSyncTarget(
  tracks: readonly PlaybackTrack[],
  playOrder: readonly number[],
  index: number,
  key: string | undefined,
): number | null {
  const inRange = index >= 0 && index < playOrder.length;
  if (key == null) return inRange ? index : null;
  const ordered = playOrder.map((i) => tracks[i] ?? null);
  const at = inRange ? ordered[index] : null;
  if (at && trackKey(at) === key) return index;
  return nearestKeyPosition(ordered, key, index);
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
    set({
      tracks,
      playOrder: order,
      currentIndex: order.length === 0 ? -1 : startIndex,
      shuffled: false,
      source,
      resumePositionMs: 0,
      generation: get().generation + 1,
    });
  },

  restoreQueue: (tracks, playOrder, currentIndex, source, shuffled) => {
    const clampedIdx =
      playOrder.length === 0 ? -1 : Math.max(0, Math.min(currentIndex, playOrder.length - 1));
    set({
      tracks,
      playOrder,
      currentIndex: clampedIdx,
      shuffled,
      source,
      resumePositionMs: 0,
      generation: get().generation + 1,
    });
  },

  enqueue: (track) => {
    const { tracks, playOrder } = get();
    set({ tracks: [...tracks, track], playOrder: [...playOrder, tracks.length] });
  },

  playNext: (track) => {
    const { tracks, playOrder, currentIndex } = get();
    const newTrackIndex = tracks.length;
    const insertAt = currentIndex + 1;
    const newOrder = [...playOrder.slice(0, insertAt), newTrackIndex, ...playOrder.slice(insertAt)];
    set({ tracks: [...tracks, track], playOrder: newOrder });
  },

  skipToNext: () => {
    const { tracks, playOrder, currentIndex, repeatMode } = get();
    if (tracks.length === 0) return null;

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

  syncCurrentIndex: (index, key) => {
    const { tracks, playOrder, currentIndex } = get();
    const target = resolveSyncTarget(tracks, playOrder, index, key);
    if (target === null || target === currentIndex) return;
    set({ currentIndex: target });
  },

  setResumePosition: (positionMs) => {
    set({ resumePositionMs: Math.max(0, positionMs) });
  },

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

  setShuffled: (shuffled) => {
    set({ shuffled });
  },

  cycleRepeatMode: () => {
    set((s) => ({ repeatMode: REPEAT_CYCLE[s.repeatMode] }));
  },

  setRepeatMode: (mode) => {
    set({ repeatMode: mode });
  },

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
      set({ ...INITIAL, generation: get().generation + 1 });
      return;
    }
    const newOrder = playOrder.filter((_, i) => i !== index).map((i) => (i > trackIdx ? i - 1 : i));
    const newCurrent =
      index < currentIndex
        ? currentIndex - 1
        : index === currentIndex
          ? Math.min(currentIndex, newOrder.length - 1)
          : currentIndex;
    set({
      tracks: newTracks,
      playOrder: newOrder,
      currentIndex: newCurrent,
      shuffled: shuffled && newTracks.length > 1,
    });
  },

  clearQueue: () => set({ ...INITIAL, generation: get().generation + 1 }),

  currentTrack: () => {
    const { tracks, playOrder, currentIndex } = get();
    if (tracks.length === 0 || currentIndex < 0) return null;
    return trackAt(tracks, playOrder, currentIndex);
  },

  hasNext: () => {
    const { playOrder, currentIndex, repeatMode } = get();
    if (playOrder.length === 0) return false;
    if (repeatMode === 'all') return true;
    return currentIndex < playOrder.length - 1;
  },

  hasPrevious: () => {
    const { playOrder, currentIndex, repeatMode } = get();
    if (playOrder.length === 0) return false;
    if (repeatMode === 'all') return true;
    return currentIndex > 0;
  },
}));

export function orderedQueueTracks(state: {
  tracks: readonly PlaybackTrack[];
  playOrder: readonly number[];
}): PlaybackTrack[] {
  return state.playOrder.map((i) => state.tracks[i]).filter((t): t is PlaybackTrack => t != null);
}
