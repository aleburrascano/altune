import type { PlaybackPort } from '@features/playback/ports/playbackPort';

let active: PlaybackPort | null = null;

export function playbackPort(): PlaybackPort {
  if (active === null) throw new Error('no playback port installed');
  return active;
}

export function installPlaybackPort(port: PlaybackPort): () => void {
  const previous = active;
  active = port;
  return () => {
    active = previous;
  };
}
