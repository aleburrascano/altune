import type { PlaybackTrack } from './types';

declare const trackKeyBrand: unique symbol;
export type TrackKey = string & { readonly [trackKeyBrand]: true };

export function trackKey(track: PlaybackTrack): TrackKey {
  return (
    track.source.kind === 'library'
      ? `library:${track.source.trackId}`
      : `preview:${track.source.previewUrl}`
  ) as TrackKey;
}
