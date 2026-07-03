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
  toggleShuffle: () => void;
}

export function useQueuePlayback(): QueuePlaybackControls {
  const loadQueue = useQueueStore((s) => s.loadQueue);
  const { startQueue, skipNext, skipPrevious, reorderUpcoming, appendToQueue, insertNext } =
    usePlayback();

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

  return { playFromList, playTrack, addToQueue, playNext, skipToNext, skipToPrevious, toggleShuffle };
}
