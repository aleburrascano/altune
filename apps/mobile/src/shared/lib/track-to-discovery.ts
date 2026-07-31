import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { TrackResponse } from '@shared/api-client/types';

export function trackToDiscoveryResult(track: TrackResponse): DiscoveryResult {
  return {
    kind: 'track',
    title: track.title,
    subtitle: track.artist,
    image_url: track.artwork_url || null,
    confidence: 'high',
    sources: [],
    extras: {
      ...(track.album != null ? { album: track.album } : {}),
      ...(track.duration_seconds != null ? { duration_seconds: track.duration_seconds } : {}),
      ...(track.track_number != null ? { track_position: track.track_number } : {}),
      ...(track.featured_artists && track.featured_artists.length > 0
        ? { featured_artists: track.featured_artists }
        : {}),
      acquisition_status: track.acquisition_status,
      track_id: track.id,
    },
  };
}
