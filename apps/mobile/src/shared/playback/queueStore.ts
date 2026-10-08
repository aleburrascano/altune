import { create } from 'zustand';

import {
  ascending,
  fisherYates,
  identityOrder,
  isPositionIn,
  reorderTail,
  REPEAT_CYCLE,
  resolveSyncTarget,
  trackAt,
  withAppended,
  withoutTrack,
  withoutUpcoming,
} from '@shared/playback/queueOrder';
import type { QueueState } from '@shared/playback/queueOrder';
import type { PlaybackTrack, QueueSource, RepeatMode } from '@shared/playback/types';

export interface QueueView {
  ordered: PlaybackTrack[];
  currentIndex: number;
}

interface RestoreQueueOptions {
  tracks: readonly PlaybackTrack[];
  playOrder: readonly number[];
  currentIndex: number;
  source: QueueSource | null;
  shuffled: boolean;
}

interface QueueActions {
  loadQueue: (
    tracks: readonly PlaybackTrack[],
    startIndex: number,
    source: QueueSource | null,
  ) => QueueView;
  loadShuffled: (tracks: readonly PlaybackTrack[], source: QueueSource | null) => QueueView;
  restoreQueue: (options: RestoreQueueOptions) => void;
  enqueue: (track: PlaybackTrack) => void;
  enqueueMany: (tracks: readonly PlaybackTrack[]) => PlaybackTrack[];
  playNext: (track: PlaybackTrack) => void;
  skipToNext: () => PlaybackTrack | null;
  skipToPrevious: () => PlaybackTrack | null;
  skipToIndex: (index: number) => PlaybackTrack | null;
  syncCurrentIndex: (index: number, key?: string) => void;
  setResumePosition: (positionMs: number) => void;
  toggleShuffle: () => PlaybackTrack[];
  setShuffled: (shuffled: boolean) => void;
  cycleRepeatMode: () => void;
  setRepeatMode: (mode: RepeatMode) => void;
  reorderQueue: (fromIndex: number, toIndex: number) => PlaybackTrack[];
  removeFromQueue: (index: number) => void;
  clearUpcoming: () => void;
  clearQueue: () => void;
  currentTrack: () => PlaybackTrack | null;
  hasNext: () => boolean;
  hasPrevious: () => boolean;
}

export type QueueStore = QueueState & QueueActions;

const INITIAL: QueueState = {
  tracks: [],
  playOrder: [],
  upNext: [],
  appended: [],
  currentIndex: -1,
  repeatMode: 'off',
  shuffled: false,
  source: null,
  resumePositionMs: 0,
  generation: 0,
};

export const useQueueStore = create<QueueStore>((set, get) => ({
  ...INITIAL,

  loadQueue: (tracks, startIndex, source) => {
    const order = identityOrder(tracks.length);
    set({
      tracks,
      playOrder: order,
      upNext: [],
      appended: [],
      currentIndex: order.length === 0 ? -1 : startIndex,
      shuffled: false,
      source,
      resumePositionMs: 0,
      generation: get().generation + 1,
    });
    return queueView(get());
  },

  loadShuffled: (tracks, source) => {
    const order = identityOrder(tracks.length);
    fisherYates(order);
    set({
      tracks,
      playOrder: order,
      upNext: [],
      appended: [],
      currentIndex: tracks.length === 0 ? -1 : 0,
      shuffled: true,
      source,
      resumePositionMs: 0,
      generation: get().generation + 1,
    });
    return queueView(get());
  },

  restoreQueue: ({ tracks, playOrder, currentIndex, source, shuffled }) => {
    const clampedIdx =
      playOrder.length === 0 ? -1 : Math.max(0, Math.min(currentIndex, playOrder.length - 1));
    set({
      tracks,
      playOrder,
      upNext: [],
      appended: [],
      currentIndex: clampedIdx,
      shuffled,
      source,
      resumePositionMs: 0,
      generation: get().generation + 1,
    });
  },

  enqueue: (track) => set((s) => withAppended(s, [track])),

  enqueueMany: (tracks) => {
    set((s) => withAppended(s, tracks));
    return upcomingTracks(get());
  },

  playNext: (track) => {
    const { tracks, playOrder, currentIndex, upNext } = get();
    const newTrackIndex = tracks.length;
    const insertAt = currentIndex + 1;
    const newOrder = [...playOrder.slice(0, insertAt), newTrackIndex, ...playOrder.slice(insertAt)];
    set({
      tracks: [...tracks, track],
      playOrder: newOrder,
      upNext: [...upNext, newTrackIndex],
    });
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
    if (!isPositionIn(playOrder.length, index)) return null;
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
    const { tracks, playOrder, currentIndex, shuffled, upNext, appended } = get();
    if (tracks.length <= 1) return upcomingTracks(get());

    const arrange = shuffled ? ascending : fisherYates;
    set({
      playOrder: reorderTail(playOrder, currentIndex, { upNext, appended }, arrange),
      shuffled: !shuffled,
    });
    return upcomingTracks(get());
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
    if (fromIndex === toIndex) return upcomingTracks(get());
    if (!isPositionIn(playOrder.length, fromIndex)) return upcomingTracks(get());
    if (!isPositionIn(playOrder.length, toIndex)) return upcomingTracks(get());
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
    return upcomingTracks(get());
  },

  removeFromQueue: (index) => {
    const { tracks, playOrder, currentIndex, shuffled, upNext, appended } = get();
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
      upNext: withoutTrack(upNext, trackIdx),
      appended: withoutTrack(appended, trackIdx),
      currentIndex: newCurrent,
      shuffled: shuffled && newTracks.length > 1,
    });
  },

  clearUpcoming: () => {
    const state = get();
    const kept = state.playOrder.slice(0, state.currentIndex + 1);
    if (kept.length === state.playOrder.length) return;
    if (kept.length === 0) {
      set({ ...INITIAL, generation: state.generation + 1 });
      return;
    }
    set(withoutUpcoming(state, kept));
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

function queueView(state: QueueState): QueueView {
  return { ordered: orderedQueueTracks(state), currentIndex: state.currentIndex };
}

function upcomingTracks(state: QueueState): PlaybackTrack[] {
  return orderedQueueTracks(state).slice(state.currentIndex + 1);
}

export function orderedQueueTracks(state: {
  tracks: readonly PlaybackTrack[];
  playOrder: readonly number[];
}): PlaybackTrack[] {
  return state.playOrder.map((i) => state.tracks[i]).filter((t): t is PlaybackTrack => t != null);
}
