/**
 * applyServerEvent — pure router from a server event to a cache effect.
 *
 * Acquisition events patch the track by id across all caches (cache-as-truth,
 * so every screen is coherent at once). Membership/list events invalidate the
 * affected lists. Unknown types are ignored. Extracted from useServerEvents so
 * the routing is unit-testable without the SSE transport or AppState.
 */

import type { QueryClient } from '@tanstack/react-query';

import { patchTrackInCaches } from './trackCachePatch';
import type { ServerEvent } from './sse-client';

const INVALIDATION_MAP: Record<string, string[][]> = {
  track_added_to_library: [['library-home'], ['library']],
  track_deleted: [['library-home'], ['library'], ['playlists']],
  playlist_created: [['playlists']],
  playlist_deleted: [['playlists'], ['playlist']],
  track_added_to_playlist: [['playlist'], ['playlists']],
  track_removed_from_playlist: [['playlist'], ['playlists']],
};

function asString(value: unknown): string | null {
  return typeof value === 'string' ? value : null;
}

export function applyServerEvent(queryClient: QueryClient, event: ServerEvent): void {
  if (event.type === 'track_acquisition_completed') {
    const trackId = asString(event.data.track_id);
    if (trackId) {
      patchTrackInCaches(queryClient, trackId, {
        acquisition_status: 'ready',
        audio_ref: asString(event.data.audio_ref),
      });
    }
    return;
  }

  if (event.type === 'track_acquisition_failed') {
    const trackId = asString(event.data.track_id);
    if (trackId) {
      patchTrackInCaches(queryClient, trackId, {
        acquisition_status: 'failed',
        failure_reason: asString(event.data.reason),
        audio_ref: null,
      });
    }
    return;
  }

  const keys = INVALIDATION_MAP[event.type];
  if (!keys) return;
  for (const queryKey of keys) {
    void queryClient.invalidateQueries({ queryKey });
  }
}
