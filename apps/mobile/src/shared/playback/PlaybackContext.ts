import { createContext } from 'react';

import type { PlaybackContextValue } from '@shared/playback/types';

export const PlaybackContext = createContext<PlaybackContextValue | null>(null);
