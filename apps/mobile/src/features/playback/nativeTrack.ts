import { type AddTrack } from 'react-native-track-player';

import type { PlaybackTrack } from '@shared/playback/types';

import { audioStreamUrl } from '@shared/api-client/audio';

export function toNativeTrack(
  track: PlaybackTrack,
  opts: { streamUrl?: string | undefined; headers?: Record<string, string> | undefined } = {},
): AddTrack {
  const base = {
    title: track.title,
    artist: track.artist,
    artwork: track.artworkUrl ?? '',
  };
  if (opts.streamUrl) return { ...base, url: opts.streamUrl };
  if (track.source.kind === 'preview') return { ...base, url: track.source.previewUrl };
  return { ...base, url: audioStreamUrl(track.source.trackId), headers: opts.headers ?? {} };
}
