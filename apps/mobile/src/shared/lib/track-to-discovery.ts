import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { TrackResponse } from '@shared/api-client/types';

/**
 * Adapts a library TrackResponse into the discovery wire shape, so a saved
 * track can flow through the same detail-handoff path as a discovery result.
 *
 * Shared by detail (album/artist library tracks) and library (row navigation)
 * once both needed the identical mapping. `track_position` is included whenever
 * the track carries a number — a harmless superset for callers that don't read it.
 */
export function trackToDiscoveryResult(track: TrackResponse): DiscoveryResult {
  return {
    kind: 'track',
    title: track.title,
    subtitle: track.artist,
    image_url: track.artwork_url ?? null,
    confidence: 'high',
    sources: [],
    extras: {
      ...(track.album != null ? { album: track.album } : {}),
      ...(track.duration_seconds != null ? { duration_seconds: track.duration_seconds } : {}),
      ...(track.track_number != null ? { track_position: track.track_number } : {}),
      acquisition_status: track.acquisition_status,
      track_id: track.id,
    },
  };
}
