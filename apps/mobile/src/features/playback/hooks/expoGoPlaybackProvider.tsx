import { useMemo, type ReactNode } from 'react';

import { PlaybackContext } from '@shared/playback/PlaybackContext';
import { useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackContextValue } from '@shared/playback/types';

// Without this, a control that resolves successfully and changes nothing is
// indistinguishable from a broken queue UI while developing in Expo Go (#1738).
function warnControlSkipped(control: string): void {
  if (!__DEV__) return;
  console.warn(
    `[playback] ${control}() skipped: audio is disabled in Expo Go — use a dev build (expo-dev-client) to test playback`,
  );
}

export function ExpoGoPlaybackProvider({ children }: { children: ReactNode }) {
  const value: PlaybackContextValue = useMemo(
    () => ({
      status: 'idle',
      track: null,
      positionMs: 0,
      durationMs: 0,
      errorMessage: null,
      errorKind: null,
      play: async () => warnControlSkipped('play'),
      startQueue: async () => warnControlSkipped('startQueue'),
      reorderUpcoming: async () => warnControlSkipped('reorderUpcoming'),
      appendToQueue: async () => warnControlSkipped('appendToQueue'),
      insertNext: async () => warnControlSkipped('insertNext'),
      skipToQueueIndex: async () => warnControlSkipped('skipToQueueIndex'),
      skipNext: async () => {
        useQueueStore.getState().skipToNext();
      },
      skipPrevious: async () => {
        useQueueStore.getState().skipToPrevious();
      },
      removeQueueIndex: async () => warnControlSkipped('removeQueueIndex'),
      pause: () => warnControlSkipped('pause'),
      resume: () => warnControlSkipped('resume'),
      seekTo: () => warnControlSkipped('seekTo'),
      setRate: () => warnControlSkipped('setRate'),
      stop: () => warnControlSkipped('stop'),
      retry: () => warnControlSkipped('retry'),
    }),
    [],
  );

  return <PlaybackContext.Provider value={value}>{children}</PlaybackContext.Provider>;
}
