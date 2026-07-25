import type { PlaybackTrack } from './types';

export function trackKey(track: PlaybackTrack): string {
  return track.source.kind === 'library'
    ? `library:${track.source.trackId}`
    : `preview:${track.source.previewUrl}`;
}
