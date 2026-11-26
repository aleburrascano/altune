import { createContext, use } from 'react';
import type { ReactElement, ReactNode } from 'react';

import type { NativePlayback } from '@features/player/ports/nativePlayback';

const NativePlaybackContext = createContext<NativePlayback | null>(null);

interface NativePlaybackProviderProps {
  port: NativePlayback;
  children: ReactNode;
}

export function NativePlaybackProvider({
  port,
  children,
}: NativePlaybackProviderProps): ReactElement {
  return <NativePlaybackContext.Provider value={port}>{children}</NativePlaybackContext.Provider>;
}

export function useNativePlayback(): NativePlayback {
  const port = use(NativePlaybackContext);
  if (port === null) throw new Error('no native playback provided: mount NativePlaybackProvider');
  return port;
}
