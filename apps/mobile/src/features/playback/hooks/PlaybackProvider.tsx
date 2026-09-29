import type { ComponentType, ReactElement, ReactNode } from 'react';

import { playsThroughTrackPlayer } from '../playsThroughTrackPlayer';

type ProviderComponent = ComponentType<{ children: ReactNode }>;

function selectPlaybackProvider(): ProviderComponent {
  if (playsThroughTrackPlayer)
    return require('../native/trackPlayerProvider').TrackPlayerPlaybackProvider;
  return require('../native/expoGoPlaybackProvider').ExpoGoPlaybackProvider;
}

const PlaybackProviderImpl = selectPlaybackProvider();

export function PlaybackProvider({ children }: { children: ReactNode }): ReactElement {
  return <PlaybackProviderImpl>{children}</PlaybackProviderImpl>;
}
