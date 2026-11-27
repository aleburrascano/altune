import { httpPlaybackPort } from '@features/player/adapters/http/httpPlaybackPort';
import { playsThroughTrackPlayer } from '@features/player/core/playsThroughTrackPlayer';
import { installPlaybackPort } from '@features/player/ports/activePlaybackPort';
import type { PlaybackPort } from '@features/player/ports/playbackPort';

export interface StartPlaybackServiceDeps {
  port: PlaybackPort;
  playsThroughTrackPlayer: boolean;
  registerPlaybackService: () => void;
}

export function createStartPlaybackService(deps: StartPlaybackServiceDeps): () => void {
  return () => {
    installPlaybackPort(deps.port);
    if (!deps.playsThroughTrackPlayer) return;
    deps.registerPlaybackService();
  };
}

export const startPlaybackService = createStartPlaybackService({
  port: httpPlaybackPort,
  playsThroughTrackPlayer,
  registerPlaybackService: () => {
    require('@features/player/adapters/native/playbackService').registerPlaybackService();
  },
});
