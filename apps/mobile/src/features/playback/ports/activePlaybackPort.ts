import { httpPlaybackPort } from '@features/playback/adapters/http/httpPlaybackPort';
import type { PlaybackPort } from '@features/playback/ports/playbackPort';

let active: PlaybackPort = httpPlaybackPort;

export function playbackPort(): PlaybackPort {
  return active;
}

export function installPlaybackPort(port: PlaybackPort): () => void {
  const previous = active;
  active = port;
  return () => {
    active = previous;
  };
}
