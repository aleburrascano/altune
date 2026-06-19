import { useCallback } from 'react';

import { useQueueStore } from './queueStore';
import type { PlaybackTrack, QueueSource } from './types';
import { usePlayback } from './usePlayback';

interface QueuePlaybackControls {
  playFromList: (tracks: readonly PlaybackTrack[], startIndex: number, source: QueueSource | null) => void;
  playTrack: (track: PlaybackTrack) => void;
  skipToNext: () => void;
  skipToPrevious: () => void;
}

export function useQueuePlayback(): QueuePlaybackControls {
  const { play } = usePlayback();
  const loadQueue = useQueueStore((s) => s.loadQueue);

  const playFromList = useCallback(
    (tracks: readonly PlaybackTrack[], startIndex: number, source: QueueSource | null) => {
      loadQueue(tracks, startIndex, source);
      const track = tracks[startIndex];
      if (track) void play(track);
    },
    [loadQueue, play],
  );

  const playTrack = useCallback(
    (track: PlaybackTrack) => {
      loadQueue([track], 0, null);
      void play(track);
    },
    [loadQueue, play],
  );

  const skipToNext = useCallback(() => {
    const nextTrack = useQueueStore.getState().skipToNext();
    if (nextTrack) void play(nextTrack);
  }, [play]);

  const skipToPrevious = useCallback(() => {
    const prevTrack = useQueueStore.getState().skipToPrevious();
    if (prevTrack) void play(prevTrack);
  }, [play]);

  return { playFromList, playTrack, skipToNext, skipToPrevious };
}
