import type { ReactElement, ReactNode } from 'react';

import { NativePlaybackProvider, trackPlayerPlayback } from '@features/player';

interface WithNativePlaybackProps {
  children: ReactNode;
}

export function withNativePlayback({ children }: WithNativePlaybackProps): ReactElement {
  return <NativePlaybackProvider port={trackPlayerPlayback}>{children}</NativePlaybackProvider>;
}
