import type { ReactElement, ReactNode } from 'react';

import { WebPlaybackProvider } from '@features/playback/web/webPlaybackProvider';

export function PlaybackProvider({ children }: { children: ReactNode }): ReactElement {
  return <WebPlaybackProvider>{children}</WebPlaybackProvider>;
}
