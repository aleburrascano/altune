import type { ReactNode } from 'react';

import { ExpoGoPlaybackProvider } from './expoGoPlaybackProvider';

export function TrackPlayerPlaybackProvider({ children }: { children: ReactNode }) {
  return <ExpoGoPlaybackProvider>{children}</ExpoGoPlaybackProvider>;
}
