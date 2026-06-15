import { createContext } from 'react';

import type { PlaybackContextValue } from './types';

export const PlaybackContext = createContext<PlaybackContextValue | null>(null);
