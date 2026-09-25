import { create } from 'zustand';

import { trackKey } from './trackKey';
import type { PlaybackTrack, QueueSource, RepeatMode } from './types';

interface QueueState {
  tracks: readonly PlaybackTrack[];
  playOrder: readonly number[];
  upNext: readonly number[];
  appended: readonly number[];
  currentIndex: number;
  repeatMode: RepeatMode;
  shuffled: boolean;
  source: QueueSource | null;
  resumePositionMs: number;
  generation: number;
}

/**
 * The ordered queue as the native player needs it, returned by the mutators that
 * rebuild or reorder the queue. Returning it from the mutation itself replaces the
 * old mutate-then-`getState()` pair at each call site, whose order nothing enforced:
 * reading before mutating compiled fine and sent the native player stale tracks.
 */
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
  /** Appends every track in one mutation; returns the upcoming tracks native must hold after it. */
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
  /** Drops every upcoming Track in one mutation, whatever the queue's length. */
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

function identityOrder(length: number): number[] {
  return Array.from({ length }, (_, i) => i);
}

interface UserQueued {
  upNext: readonly number[];
  appended: readonly number[];
}

function reorderTail(
  order: readonly number[],
  keepThrough: number,
  queued: UserQueued,
  arrange: (rest: number[]) => void,
): number[] {
  const tail = order.slice(keepThrough + 1);
  const nextIds = new Set(queued.upNext);
  const lastIds = new Set(queued.appended);
  const rest = tail.filter((i) => !nextIds.has(i) && !lastIds.has(i));
  arrange(rest);
  const next = tail.filter((i) => nextIds.has(i));
  const last = tail.filter((i) => lastIds.has(i));
  return [...order.slice(0, keepThrough + 1), ...next, ...rest, ...last];
}

function fisherYates(items: number[]): void {
  for (let i = items.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    const tmp = items[i]!;
    items[i] = items[j]!;
    items[j] = tmp;
  }
}

function ascending(items: number[]): void {
  items.sort((a, b) => a - b);
}

// One copy of each queue array however many tracks arrive, so appending a whole selection
// costs a single mutation rather than one per track (and O(n) copying rather than O(n²)).
function withAppended(state: QueueState, tracks: readonly PlaybackTrack[]): Partial<QueueState> {
  const added = tracks.map((_, i) => state.tracks.length + i);
  return {
    tracks: [...state.tracks, ...tracks],
    playOrder: [...state.playOrder, ...added],
    appended: [...state.appended, ...added],
  };
}

function withoutTrack(indices: readonly number[], removed: number): number[] {
  return indices.filter((i) => i !== removed).map((i) => (i > removed ? i - 1 : i));
}

function survivingPositions(
  indices: readonly number[],
  renumbered: ReadonlyMap<number, number>,
): number[] {
  return indices.flatMap((i) => {
    const at = renumbered.get(i);
    return at == null ? [] : [at];
  });
}

// Costs one pass over the Tracks that stay, however many are dropped. Dropping them from
// `tracks` without renumbering would leave playOrder pointing past the end — the store's
// standing law is that playOrder is an index permutation over tracks.
function withoutUpcoming(state: QueueState, kept: readonly number[]): Partial<QueueState> {
  const renumbered = new Map(kept.map((trackIdx, position) => [trackIdx, position]));
  return {
    tracks: kept.map((i) => state.tracks[i]!),
    playOrder: identityOrder(kept.length),
    upNext: survivingPositions(state.upNext, renumbered),
    appended: survivingPositions(state.appended, renumbered),
    shuffled: state.shuffled && kept.length > 1,
  };
}

// Range checks alone let NaN through (every comparison with NaN is false) and
// let fractions through that splice silently truncates, so require an integer.
function isPositionIn(length: number, index: number): boolean {
  return Number.isInteger(index) && index >= 0 && index < length;
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
