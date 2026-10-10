import { httpPlaybackPort } from '@features/playback/adapters/http/httpPlaybackPort';
import { playsThroughTrackPlayer } from '@features/playback/domain/playsThroughTrackPlayer';
import { installPlaybackPort } from '@features/playback/ports/activePlaybackPort';

export function startPlaybackService(): void {
  installPlaybackPort(httpPlaybackPort);
  if (!playsThroughTrackPlayer) return;
  require('@features/playback/native/playbackService').registerPlaybackService();
}
