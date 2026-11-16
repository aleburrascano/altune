import { createContext, use } from 'react';

import type { PlaybackContextValue } from '@shared/playback/playbackContextValue';

export const PlaybackContext = createContext<PlaybackContextValue | null>(null);

export function usePlayback(): PlaybackContextValue {
  const ctx = use(PlaybackContext);
  if (!ctx) {
    throw new Error('usePlayback must be used within a PlaybackProvider');
  }
  return ctx;
}
