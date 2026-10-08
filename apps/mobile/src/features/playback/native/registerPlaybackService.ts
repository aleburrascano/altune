import TrackPlayer from 'react-native-track-player';

import { onSignOut } from '@shared/session/signOutCleanup';

import { playbackService, resetPlaybackForSignOut } from '@features/playback/native/service';

export function registerPlaybackService(): void {
  TrackPlayer.registerPlaybackService(() => playbackService);
  onSignOut(resetPlaybackForSignOut);
}
