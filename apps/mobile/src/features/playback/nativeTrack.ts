import { type AddTrack } from 'react-native-track-player';

import type { PlaybackTrack } from '@shared/playback/types';

import { audioStreamUrl } from './api/audio';

// AIDEV-NOTE: The single PlaybackTrack -> native AddTrack builder. `streamUrl`
// overrides the URL — a resolved presigned URL (self-authorizing, so it carries
// no auth headers) or a downloaded local file path. Without it, library tracks
// fall back to the authenticated proxy (headers required) and previews stream
// their external URL directly. Both loadNativeTrack (batch queue loads) and
// audioPrefetch (per-track download/repair) route through here so the
// presigned-else-proxy fallback ladder lives in exactly one place.
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
