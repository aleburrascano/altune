import TrackPlayer from 'react-native-track-player';

import { playbackService } from './service';

export function registerPlaybackService(): void {
  TrackPlayer.registerPlaybackService(() => playbackService);
}
