import { playsThroughTrackPlayer } from '@features/playback/domain/playsThroughTrackPlayer';

export function startPlaybackService(): void {
  if (!playsThroughTrackPlayer) return;
  require('@features/playback/native/playbackService').registerPlaybackService();
}
