import TrackPlayer from 'react-native-track-player';

import { playbackService } from './service';

// AIDEV-NOTE: Isolates the track-player playback-service registration so it can
// be required conditionally (skipped in Expo Go, where the native module is
// absent). Called from the root layout only when !isExpoGo.
export function registerPlaybackService(): void {
  TrackPlayer.registerPlaybackService(() => playbackService);
}
