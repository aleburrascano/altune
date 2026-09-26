import type { ComponentType, ReactElement, ReactNode } from 'react';
import { Platform } from 'react-native';

import { playsThroughTrackPlayer } from '../playsThroughTrackPlayer';

type ProviderComponent = ComponentType<{ children: ReactNode }>;

function selectPlaybackProvider(): ProviderComponent {
  if (playsThroughTrackPlayer) return require('./trackPlayerProvider').TrackPlayerPlaybackProvider;
  if (Platform.OS === 'web') return require('./webPlaybackProvider').WebPlaybackProvider;
  return require('./expoGoPlaybackProvider').ExpoGoPlaybackProvider;
}

const PlaybackProviderImpl = selectPlaybackProvider();

export function PlaybackProvider({ children }: { children: ReactNode }): ReactElement {
  return <PlaybackProviderImpl>{children}</PlaybackProviderImpl>;
}
