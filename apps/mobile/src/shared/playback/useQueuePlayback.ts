import { useCallback } from 'react';

import { usePlayback } from './usePlayback';
import { orderedQueueTracks, useQueueStore } from './queueStore';
import type { PlaybackTrack, QueueSource } from './types';

interface QueuePlaybackControls {
  playFromList: (tracks: readonly PlaybackTrack[], startIndex: number, source: QueueSource | null) => void;
  playTrack: (track: PlaybackTrack) => void;
  addToQueue: (track: PlaybackTrack) => void;
  playNext: (track: PlaybackTrack) => void;
  skipToNext: () => void;
  skipToPrevious: () => void;
  skipToIndex: (index: number) => void;
  removeFromQueue: (index: number) => void;
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

  // Add to Queue: append to the end of the current queue. With no active queue
  // there is nothing to queue behind, so start playing the track instead. Store
  // mutation + native append stay in lockstep (see queueStore AIDEV-WARNING).
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

  // Play Next: insert right after the current track. Empty queue → just play it.
  // If the current track is last, currentIndex+1 is past the end, so append
  // rather than insert (RNTP's insertBeforeIndex must be within range).
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

  // Skips are native: the next track is already buffered, so the switch is
  // instant and gapless. The store's currentIndex follows from the native
  // PlaybackActiveTrackChanged event (see service.ts).
  const skipToNext = useCallback(() => {
    void skipNext();
  }, [skipNext]);

  const skipToPrevious = useCallback(() => {
    void skipPrevious();
  }, [skipPrevious]);

  // Jump to an already-loaded queue position. Store cursor + native skip stay in
  // lockstep (the native track is already buffered, so the switch is instant).
  const skipToIndex = useCallback(
    (index: number) => {
      useQueueStore.getState().skipToIndex(index);
      void skipToQueueIndex(index);
    },
    [skipToQueueIndex],
  );

  // Remove one queued track. Store mutation + native remove stay in lockstep
  // (see queueStore removeFromQueue AIDEV-WARNING); the playing track is never
  // the target here — callers pass an upcoming position.
  const removeFromQueue = useCallback(
    (index: number) => {
      useQueueStore.getState().removeFromQueue(index);
      void removeQueueIndex(index);
    },
    [removeQueueIndex],
  );

  // Clear every upcoming track (everything after the current one). Reads the
  // store at call time — a confirm dialog can sit open across auto-advances, and
  // a stale currentIndex would delete the playing track. Descending iteration
  // keeps the indices valid as entries are removed; store + native stay locked.
  const clearUpcoming = useCallback(() => {
    const s = useQueueStore.getState();
    for (let i = s.playOrder.length - 1; i > s.currentIndex; i--) {
      s.removeFromQueue(i);
      void removeQueueIndex(i);
    }
  }, [removeQueueIndex]);

  // Shuffle only reorders the upcoming tracks (queueStore keeps the current
  // track's position), so the native side just replaces the tracks after the
  // current one — the playing track is never touched, so no re-buffer and no
  // UI/audio desync. Native index still mirrors the store's play order.
  const toggleShuffle = useCallback(() => {
    useQueueStore.getState().toggleShuffle();
    const s = useQueueStore.getState();
    const upcoming = orderedQueueTracks(s).slice(s.currentIndex + 1);
    void reorderUpcoming(upcoming);
  }, [reorderUpcoming]);

  // Repeat mode is mirrored onto the native player by an effect in the provider,
  // so this only advances the store — exposed through the facade so callers stop
  // reaching into the store directly for it.
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
    clearUpcoming,
    toggleShuffle,
    cycleRepeatMode,
  };
}
