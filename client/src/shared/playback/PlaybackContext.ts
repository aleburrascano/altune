import { createContext } from 'react';

import type { PlaybackContextValue } from '@shared/playback/playbackContextValue';

export const PlaybackContext = createContext<PlaybackContextValue | null>(null);
