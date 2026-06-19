import type { TrackResponse } from '@shared/api-client/types';

import type { PlaybackTrack } from './types';

export function toPlaybackTrack(t: TrackResponse): PlaybackTrack {
  return {
    source: { kind: 'library', trackId: t.id },
    title: t.title,
    artist: t.artist,
    artworkUrl: t.artwork_url ?? null,
    durationSeconds: t.duration_seconds ?? undefined,
  };
}
