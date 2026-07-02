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
  const { startQueue, skipNext, skipPrevious, positionMs } = usePlayback();

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

  // Toggling shuffle reorders playOrder, so the native queue must be rebuilt to
  // match — otherwise the store's index (which follows native by position) maps
  // onto the new order and the UI desyncs from audio. The current track stays
  // current (pinned at playOrder[0]); resume it at its live position so shuffle
  // doesn't restart the song from 0.
  const toggleShuffle = useCallback(() => {
    const resumeAtMs = positionMs;
    useQueueStore.getState().toggleShuffle();
    const s = useQueueStore.getState();
    void startQueue(orderedQueueTracks(s), s.currentIndex, { startPositionMs: resumeAtMs });
  }, [startQueue, positionMs]);

  return { playFromList, playTrack, skipToNext, skipToPrevious, toggleShuffle };
}
