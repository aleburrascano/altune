import { useCallback, useEffect, useRef } from 'react';

import type { QueueStore } from '@shared/playback';
import { useAppStateChange } from '@shared/lifecycle';

import { useNativePlayback } from '@features/player/view/NativePlaybackProvider';

const SAVE_INTERVAL_MS = 15_000;

export function useQueueResume() {
  const playback = useNativePlayback();
  const saveTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const restoredRef = useRef(false);
  const placeholderGenerationRef = useRef<number | null>(null);
  const saverRef = useRef<(() => Promise<void>) | null>(null);
  const save = useCallback((): Promise<void> => {
    const isSkippable = (state: QueueStore): boolean =>
      state.tracks.length === 0 ||
      placeholderGenerationRef.current === state.generation ||
      state.currentTrack()?.source.kind !== 'library';
    saverRef.current ??= playback.createQueueSaver(isSkippable);
    return saverRef.current();
  }, [playback]);

  useEffect(() => {
    if (restoredRef.current) return;
    restoredRef.current = true;

    void playback.restoreSavedQueue((generation) => {
      placeholderGenerationRef.current = generation;
    });
  }, [playback]);

  useEffect(() => {
    saveTimerRef.current = setInterval(() => {
      void save();
    }, SAVE_INTERVAL_MS);
    return () => {
      if (saveTimerRef.current) clearInterval(saveTimerRef.current);
    };
  }, [save]);

  useAppStateChange((state) => {
    if (state === 'background' || state === 'inactive') void save();
  });
}
