import { useMemo, type ReactNode } from 'react';

import { PlaybackContext } from '@shared/playback/PlaybackContext';
import type { PlaybackContextValue } from '@shared/playback/types';

// AIDEV-NOTE: No-op playback backend used in Expo Go, where
// react-native-track-player's native module is unavailable. Provides a stable
// idle context so the whole app boots and is testable; audio controls are
// inert. Use a development build (expo-dev-client) to exercise real playback.

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
      pause: () => {},
      resume: () => {},
      seekTo: () => {},
      stop: () => {},
      retry: () => {},
    }),
    [],
  );

  return <PlaybackContext.Provider value={value}>{children}</PlaybackContext.Provider>;
}
