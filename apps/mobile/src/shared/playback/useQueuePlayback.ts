import { useCallback } from 'react';

import { usePlayback } from './usePlayback';
import { orderedQueueTracks, useQueueStore } from './queueStore';
import type { PlaybackTrack, QueueSource } from './types';

interface QueuePlaybackControls {
  playFromList: (tracks: readonly PlaybackTrack[], startIndex: number, source: QueueSource | null) => void;
  playTrack: (track: PlaybackTrack) => void;
  skipToNext: () => void;
  skipToPrevious: () => void;
  toggleShuffle: () => void;
}

export function useQueuePlayback(): QueuePlaybackControls {
  const loadQueue = useQueueStore((s) => s.loadQueue);
  const { startQueue, skipNext, skipPrevious } = usePlayback();

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

  // Skips are native: the next track is already buffered, so the switch is
  // instant and gapless. The store's currentIndex follows from the native
  // PlaybackActiveTrackChanged event (see service.ts).
  const skipToNext = useCallback(() => {
    void skipNext();
  }, [skipNext]);

  const skipToPrevious = useCallback(() => {
    void skipPrevious();
  }, [skipPrevious]);

  const toggleShuffle = useCallback(() => {
    useQueueStore.getState().toggleShuffle();
    const s = useQueueStore.getState();
    void startQueue(orderedQueueTracks(s), s.currentIndex);
  }, [startQueue]);

  return { playFromList, playTrack, skipToNext, skipToPrevious, toggleShuffle };
}
