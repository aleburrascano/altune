import type { ComponentType, ReactElement, ReactNode } from 'react';

import { isExpoGo } from '@shared/playback/isExpoGo';

const PlaybackProviderImpl: ComponentType<{ children: ReactNode }> = isExpoGo
  ? require('./expoGoPlaybackProvider').ExpoGoPlaybackProvider
  : require('./trackPlayerProvider').TrackPlayerPlaybackProvider;

export function PlaybackProvider({ children }: { children: ReactNode }): ReactElement {
  return <PlaybackProviderImpl>{children}</PlaybackProviderImpl>;
}
