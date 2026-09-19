import { Image } from 'react-native';
import { type AddTrack } from 'react-native-track-player';

import type { PlaybackTrack } from '@shared/playback/types';
import { isPlayablePreviewUrl } from '@shared/playback/previewUrl';
import { trackKey } from '@shared/playback/trackKey';

import { audioStreamUrl } from '@shared/api-client/audio';
import { ContractError } from '@shared/api-client/errors';

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
