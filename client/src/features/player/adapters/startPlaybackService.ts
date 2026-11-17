import { httpPlaybackPort } from '@features/player/adapters/http/httpPlaybackPort';
import { playsThroughTrackPlayer } from '@features/player/domain/playsThroughTrackPlayer';
import { installPlaybackPort } from '@features/player/shell/activePlaybackPort';

export function startPlaybackService(): void {
  installPlaybackPort(httpPlaybackPort);
  if (!playsThroughTrackPlayer) return;
  require('@features/player/adapters/native/playbackService').registerPlaybackService();
}
