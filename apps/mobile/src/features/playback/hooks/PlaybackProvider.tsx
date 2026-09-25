import type { ComponentType, ReactElement, ReactNode } from 'react';
import { Platform } from 'react-native';

import { isExpoGo } from '@shared/playback/isExpoGo';

type ProviderComponent = ComponentType<{ children: ReactNode }>;

export const playsThroughTrackPlayer = !isExpoGo && Platform.OS !== 'web';

function selectPlaybackProvider(): ProviderComponent {
  if (playsThroughTrackPlayer) return require('./trackPlayerProvider').TrackPlayerPlaybackProvider;
  if (Platform.OS === 'web') return require('./webPlaybackProvider').WebPlaybackProvider;
  return require('./expoGoPlaybackProvider').ExpoGoPlaybackProvider;
}

const PlaybackProviderImpl = selectPlaybackProvider();

export function PlaybackProvider({ children }: { children: ReactNode }): ReactElement {
  return <PlaybackProviderImpl>{children}</PlaybackProviderImpl>;
}
