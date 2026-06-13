import { use } from 'react';

import { PlaybackContext } from './PlaybackProvider';
import type { PlaybackContextValue } from '../types';

export function usePlayback(): PlaybackContextValue {
  const ctx = use(PlaybackContext);
  if (!ctx) {
    throw new Error('usePlayback must be used within a PlaybackProvider');
  }
  return ctx;
}
