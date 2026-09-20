import { Image } from 'react-native';
import TrackPlayer, { type AddTrack } from 'react-native-track-player';

import type { PlaybackTrack } from '@shared/playback/types';
import { isPlayablePreviewUrl } from '@shared/playback/previewUrl';
import { trackKey, type TrackKey } from '@shared/playback/trackKey';

import { audioStreamUrl } from '@shared/api-client/audio';
import { ContractError } from '@shared/errors';

const ARTWORK_PLACEHOLDER = Image.resolveAssetSource(
  require('../../../assets/artwork-placeholder.png'),
).uri;

// A preview url is the one outbound target here that a third party supplies, and it reaches a
// PlaybackSource from several screens, so the scheme is re-checked at this sink: no construction
// site can forget it, and none can be edited after the check the way a stored source could.
function playablePreviewUrl(previewUrl: string): string {
  if (!isPlayablePreviewUrl(previewUrl)) throw new ContractError('PreviewUrl', 'not an https url');
  return previewUrl;
}

export function toNativeTrack(
  track: PlaybackTrack,
  opts: { streamUrl?: string | undefined; headers?: Record<string, string> | undefined } = {},
): AddTrack {
  const base = {
    id: trackKey(track),
    title: track.title,
    artist: track.artist,
    artwork: track.artworkUrl ?? ARTWORK_PLACEHOLDER,
  };
  if (opts.streamUrl) return { ...base, url: opts.streamUrl };
  if (track.source.kind === 'preview') {
    return { ...base, url: playablePreviewUrl(track.source.previewUrl) };
  }
  return { ...base, url: audioStreamUrl(track.source.trackId), headers: opts.headers ?? {} };
}

/**
 * The key of the track the native player is on, or undefined when it cannot name one:
 * nothing is active, or the call caught a player torn down under it. Every native entry
 * is written above with `trackKey(track)` as its id, so this is the one place an id is
 * read back, and the one place it is narrowed to a TrackKey.
 */
export function activeNativeTrackId(): Promise<TrackKey | undefined> {
  return TrackPlayer.getActiveTrack().then(
    (active) => (typeof active?.id === 'string' ? (active.id as TrackKey) : undefined),
    () => undefined,
  );
}
