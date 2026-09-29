import TrackPlayer from 'react-native-track-player';

import { onSignOut } from '@shared/session/signOutCleanup';

import { playbackService, resetPlaybackForSignOut } from './service';

export function registerPlaybackService(): void {
  TrackPlayer.registerPlaybackService(() => playbackService);
  onSignOut(resetPlaybackForSignOut);
}
