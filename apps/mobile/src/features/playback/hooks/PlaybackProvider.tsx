import type { ComponentType, ReactElement, ReactNode } from 'react';

import { isExpoGo } from '@shared/playback/isExpoGo';

// AIDEV-NOTE: Playback backend selector. In Expo Go we must not even *import*
// the track-player-backed provider — its top-level `react-native-track-player`
// import touches a native module Expo Go doesn't bundle, crashing at startup.
// `isExpoGo` is constant for the session, so the chosen impl is stable (no
// hook-order risk from the conditional require).
const PlaybackProviderImpl: ComponentType<{ children: ReactNode }> = isExpoGo
  ? // eslint-disable-next-line @typescript-eslint/no-require-imports
    require('./expoGoPlaybackProvider').ExpoGoPlaybackProvider
  : // eslint-disable-next-line @typescript-eslint/no-require-imports
    require('./trackPlayerProvider').TrackPlayerPlaybackProvider;

export function PlaybackProvider({ children }: { children: ReactNode }): ReactElement {
  return <PlaybackProviderImpl>{children}</PlaybackProviderImpl>;
}
