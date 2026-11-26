import type { ComponentType, ReactElement, ReactNode } from 'react';

import { playsThroughTrackPlayer } from '@features/player/domain/playsThroughTrackPlayer';

type ProviderComponent = ComponentType<{ children: ReactNode }>;

function selectPlaybackProvider(): ProviderComponent {
  if (playsThroughTrackPlayer)
    return require('@features/player/view/native/TrackPlayerPlaybackProvider')
      .TrackPlayerPlaybackProvider;
  return require('@features/player/view/native/ExpoGoPlaybackProvider').ExpoGoPlaybackProvider;
}

const PlaybackProviderImpl = selectPlaybackProvider();

export function PlaybackProvider({ children }: { children: ReactNode }): ReactElement {
  return <PlaybackProviderImpl>{children}</PlaybackProviderImpl>;
}
