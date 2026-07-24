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
  playTrack: (track: PlaybackTrack) => void;
  addToQueue: (track: PlaybackTrack) => void;
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
  const loadQueue = useQueueStore((s) => s.loadQueue);
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
      loadQueue(tracks, startIndex, source);
      const s = useQueueStore.getState();
      void startQueue(orderedQueueTracks(s), s.currentIndex);
    },
    [loadQueue, startQueue],
  );

  const playTrack = useCallback(
    (track: PlaybackTrack) => {
      loadQueue([track], 0, null);
      void startQueue([track], 0);
    },
    [loadQueue, startQueue],
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
      useQueueStore.getState().reorderQueue(fromIndex, toIndex);
      const s = useQueueStore.getState();
      const upcoming = orderedQueueTracks(s).slice(s.currentIndex + 1);
      void reorderUpcoming(upcoming);
    },
    [reorderUpcoming],
  );

  const clearUpcoming = useCallback(() => {
    const s = useQueueStore.getState();
    for (let i = s.playOrder.length - 1; i > s.currentIndex; i--) {
      s.removeFromQueue(i);
      void removeQueueIndex(i);
    }
  }, [removeQueueIndex]);

  const toggleShuffle = useCallback(() => {
    useQueueStore.getState().toggleShuffle();
    const s = useQueueStore.getState();
    const upcoming = orderedQueueTracks(s).slice(s.currentIndex + 1);
    void reorderUpcoming(upcoming);
  }, [reorderUpcoming]);

  const cycleRepeatMode = useCallback(() => {
    useQueueStore.getState().cycleRepeatMode();
  }, []);

  return {
    playFromList,
    playTrack,
    addToQueue,
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
