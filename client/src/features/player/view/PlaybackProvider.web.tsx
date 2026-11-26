import type { ReactElement, ReactNode } from 'react';

import { WebPlaybackProvider } from '@features/player/view/web/WebPlaybackProvider';

export function PlaybackProvider({ children }: { children: ReactNode }): ReactElement {
  return <WebPlaybackProvider>{children}</WebPlaybackProvider>;
}
