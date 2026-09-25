import type { ComponentType, ReactElement, ReactNode } from 'react';
import { Platform } from 'react-native';

import { isExpoGo } from '@shared/playback/isExpoGo';

// Web has no native player: react-native-track-player's web build needs shaka-player and its
// module graph crashes static rendering (`expo export -p web`), so web gets the no-op provider.
const usesNoopPlayback = isExpoGo || Platform.OS === 'web';

const PlaybackProviderImpl: ComponentType<{ children: ReactNode }> = usesNoopPlayback
  ? require('./expoGoPlaybackProvider').ExpoGoPlaybackProvider
  : require('./trackPlayerProvider').TrackPlayerPlaybackProvider;

export function PlaybackProvider({ children }: { children: ReactNode }): ReactElement {
  return <PlaybackProviderImpl>{children}</PlaybackProviderImpl>;
}
