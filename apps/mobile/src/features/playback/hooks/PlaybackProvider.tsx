import type { ComponentType, ReactElement, ReactNode } from 'react';
import { Platform } from 'react-native';

import { isExpoGo } from '@shared/playback/isExpoGo';

const usesNoopPlayback = isExpoGo || Platform.OS === 'web';

const PlaybackProviderImpl: ComponentType<{ children: ReactNode }> = usesNoopPlayback
  ? require('./expoGoPlaybackProvider').ExpoGoPlaybackProvider
  : require('./trackPlayerProvider').TrackPlayerPlaybackProvider;

export function PlaybackProvider({ children }: { children: ReactNode }): ReactElement {
  return <PlaybackProviderImpl>{children}</PlaybackProviderImpl>;
}
