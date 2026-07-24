import { useMemo, type ReactNode } from 'react';

import { PlaybackContext } from '@shared/playback/PlaybackContext';
import { useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackContextValue } from '@shared/playback/types';

export function ExpoGoPlaybackProvider({ children }: { children: ReactNode }) {
  const value: PlaybackContextValue = useMemo(
    () => ({
      status: 'idle',
      track: null,
      positionMs: 0,
      durationMs: 0,
      errorMessage: null,
      play: async () => {
        if (__DEV__) {
          console.warn(
            '[playback] audio is disabled in Expo Go — use a dev build (expo-dev-client) to test playback',
          );
        }
      },
      startQueue: async () => {},
      reorderUpcoming: async () => {},
      appendToQueue: async () => {},
      insertNext: async () => {},
      skipToQueueIndex: async () => {},
      skipNext: async () => {
        useQueueStore.getState().skipToNext();
      },
      skipPrevious: async () => {
        useQueueStore.getState().skipToPrevious();
      },
      removeQueueIndex: async () => {},
      pause: () => {},
      resume: () => {},
      seekTo: () => {},
      setRate: () => {},
      stop: () => {},
      retry: () => {},
    }),
    [],
  );

  return <PlaybackContext.Provider value={value}>{children}</PlaybackContext.Provider>;
}
