import { Image } from 'react-native';
import { type AddTrack } from 'react-native-track-player';

import type { PlaybackTrack } from '@shared/playback/types';
import { trackKey } from '@shared/playback/trackKey';

import { audioStreamUrl } from '@shared/api-client/audio';

const ARTWORK_PLACEHOLDER = Image.resolveAssetSource(
  require('../../../assets/artwork-placeholder.png'),
).uri;

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
  if (track.source.kind === 'preview') return { ...base, url: track.source.previewUrl };
  return { ...base, url: audioStreamUrl(track.source.trackId), headers: opts.headers ?? {} };
}
