import { useCallback } from 'react';

import { usePlayback } from './usePlayback';
import { orderedQueueTracks, useQueueStore } from './queueStore';
import type { PlaybackTrack, QueueSource } from './types';

interface QueuePlaybackControls {
  playFromList: (
    tracks: readonly PlaybackTrack[],
    startIndex: number,
    source: QueueSource | null,
  ) => void;
  shuffleFromList: (tracks: readonly PlaybackTrack[], source: QueueSource | null) => void;
  playTrack: (track: PlaybackTrack) => void;
  addToQueue: (track: PlaybackTrack) => void;
  addToQueueMany: (tracks: readonly PlaybackTrack[]) => void;
  playNext: (track: PlaybackTrack) => void;
  skipToNext: () => void;
  skipToPrevious: () => void;
  skipToIndex: (index: number) => void;
  removeFromQueue: (index: number) => void;
  moveQueueItem: (fromIndex: number, toIndex: number) => void;
  clearUpcoming: () => void;
  toggleShuffle: () => void;
  cycleRepeatMode: () => void;
}

export function useQueuePlayback(): QueuePlaybackControls {
  const {
    startQueue,
    skipNext,
    skipPrevious,
    skipToQueueIndex,
    removeQueueIndex,
    reorderUpcoming,
    appendToQueue,
    insertNext,
  } = usePlayback();

  const playFromList = useCallback(
    (tracks: readonly PlaybackTrack[], startIndex: number, source: QueueSource | null) => {
      const { ordered, currentIndex } = useQueueStore
        .getState()
        .loadQueue(tracks, startIndex, source);
      void startQueue(ordered, currentIndex);
    },
    [startQueue],
  );

  const shuffleFromList = useCallback(
    (tracks: readonly PlaybackTrack[], source: QueueSource | null) => {
      const { ordered, currentIndex } = useQueueStore.getState().loadShuffled(tracks, source);
      if (ordered.length === 0) return;
      void startQueue(ordered, currentIndex);
    },
    [startQueue],
  );

  const playTrack = useCallback(
    (track: PlaybackTrack) => {
      useQueueStore.getState().loadQueue([track], 0, null);
      void startQueue([track], 0);
    },
    [startQueue],
  );

  const addToQueue = useCallback(
    (track: PlaybackTrack) => {
      const s = useQueueStore.getState();
      if (orderedQueueTracks(s).length === 0) {
        playTrack(track);
        return;
      }
      s.enqueue(track);
      void appendToQueue(track);
    },
    [playTrack, appendToQueue],
  );

  // A bulk add costs one store mutation and one native queue call however many tracks arrive.
  // Per-track appends copied the queue once per track and fired that many un-awaited native
  // calls — each resolving its own signed url — in a single tick (#1699).
  const addToQueueMany = useCallback(
    (tracks: readonly PlaybackTrack[]) => {
      if (tracks.length === 0) return;
      const s = useQueueStore.getState();
      if (orderedQueueTracks(s).length === 0) {
        playFromList(tracks, 0, null);
        return;
      }
      void reorderUpcoming(s.enqueueMany(tracks));
    },
    [playFromList, reorderUpcoming],
  );

  const playNext = useCallback(
    (track: PlaybackTrack) => {
      const s = useQueueStore.getState();
      const queueLength = orderedQueueTracks(s).length;
      if (queueLength === 0) {
        playTrack(track);
        return;
      }
      const insertPos = s.currentIndex + 1;
      s.playNext(track);
      if (insertPos >= queueLength) {
        void appendToQueue(track);
      } else {
        void insertNext(track, insertPos);
      }
    },
    [playTrack, appendToQueue, insertNext],
  );

  const skipToNext = useCallback(() => {
    void skipNext();
  }, [skipNext]);

  const skipToPrevious = useCallback(() => {
    void skipPrevious();
  }, [skipPrevious]);

  const skipToIndex = useCallback(
    (index: number) => {
      useQueueStore.getState().skipToIndex(index);
      void skipToQueueIndex(index);
    },
    [skipToQueueIndex],
  );

  const removeFromQueue = useCallback(
    (index: number) => {
      useQueueStore.getState().removeFromQueue(index);
      void removeQueueIndex(index);
    },
    [removeQueueIndex],
  );

  const moveQueueItem = useCallback(
    (fromIndex: number, toIndex: number) => {
      void reorderUpcoming(useQueueStore.getState().reorderQueue(fromIndex, toIndex));
    },
    [reorderUpcoming],
  );

  // Clearing costs one store mutation and one native truncate however long the queue is.
  // Removing row by row copied both queue arrays once per track — O(n²) on the JS thread —
  // behind that many serialized native removes (#1739). An empty upcoming list is exactly
  // what `reorderUpcoming` truncates the native queue to.
  const clearUpcoming = useCallback(() => {
    useQueueStore.getState().clearUpcoming();
    void reorderUpcoming([]);
  }, [reorderUpcoming]);

  const toggleShuffle = useCallback(() => {
    void reorderUpcoming(useQueueStore.getState().toggleShuffle());
  }, [reorderUpcoming]);

  const cycleRepeatMode = useCallback(() => {
    useQueueStore.getState().cycleRepeatMode();
  }, []);

  return {
    playFromList,
    shuffleFromList,
    playTrack,
    addToQueue,
    addToQueueMany,
    playNext,
    skipToNext,
    skipToPrevious,
    skipToIndex,
    removeFromQueue,
    moveQueueItem,
    clearUpcoming,
    toggleShuffle,
    cycleRepeatMode,
  };
}
