import { create } from 'zustand';

import type { PlaybackTrack, QueueSource, RepeatMode } from './types';

interface QueueState {
  tracks: readonly PlaybackTrack[];
  playOrder: readonly number[];
  currentIndex: number;
  repeatMode: RepeatMode;
  shuffled: boolean;
  source: QueueSource | null;
}

interface QueueActions {
  loadQueue: (tracks: readonly PlaybackTrack[], startIndex: number, source: QueueSource | null) => void;
  skipToNext: () => PlaybackTrack | null;
  skipToPrevious: () => PlaybackTrack | null;
  skipToIndex: (index: number) => PlaybackTrack | null;
  toggleShuffle: () => void;
  cycleRepeatMode: () => void;
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
};

function identityOrder(length: number): number[] {
  return Array.from({ length }, (_, i) => i);
}

function fisherYatesShuffle(arr: number[], pinIndex: number): number[] {
  const result = [...arr];
  const pinPos = result.indexOf(pinIndex);
  if (pinPos > 0) {
    const pinVal = result[pinPos]!;
    result[pinPos] = result[0]!;
    result[0] = pinVal;
  }
  for (let i = result.length - 1; i > 1; i--) {
    const j = 1 + Math.floor(Math.random() * i);
    const tmp = result[i]!;
    result[i] = result[j]!;
    result[j] = tmp;
  }
  return result;
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
    set({ tracks, playOrder: order, currentIndex: startIndex, shuffled: false, source });
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

  toggleShuffle: () => {
    const { tracks, playOrder, currentIndex, shuffled } = get();
    if (tracks.length <= 1) return;

    const currentTrackIndex = playOrder[currentIndex] ?? 0;
    if (shuffled) {
      const order = identityOrder(tracks.length);
      const newCurrent = order.indexOf(currentTrackIndex);
      set({ playOrder: order, currentIndex: newCurrent >= 0 ? newCurrent : 0, shuffled: false });
    } else {
      const newOrder = fisherYatesShuffle(identityOrder(tracks.length), currentTrackIndex);
      set({ playOrder: newOrder, currentIndex: 0, shuffled: true });
    }
  },

  cycleRepeatMode: () => {
    set((s) => ({ repeatMode: REPEAT_CYCLE[s.repeatMode] }));
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

export const selectCurrentTrack = (s: QueueStore): PlaybackTrack | null =>
  s.currentTrack();
export const selectHasNext = (s: QueueStore): boolean => s.hasNext();
export const selectHasPrevious = (s: QueueStore): boolean => s.hasPrevious();
export const selectShuffled = (s: QueueStore): boolean => s.shuffled;
export const selectRepeatMode = (s: QueueStore): RepeatMode => s.repeatMode;
export const selectQueueLength = (s: QueueStore): number => s.playOrder.length;
export const selectSource = (s: QueueStore): QueueSource | null => s.source;
