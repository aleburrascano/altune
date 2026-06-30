  /**
 * applyServerEvent — pure router from a server event to a cache effect.
 *
 * Acquisition events patch the track by id across all caches (cache-as-truth,
 * so every screen — incl. the playlist that was stuck on 'pending' — is
 * coherent at once), AND invalidate the library lists. The invalidation is kept
 * deliberately: it re-syncs the detail save-control (which reads the library
 * cache imperatively) and acts as a reconciliation backstop. Membership/list
 * events invalidate the affected lists. Unknown types are ignored. Extracted
 * from useServerEvents so the routing is unit-testable without the SSE
 * transport or AppState.
 */

import type { QueryClient } from '@tanstack/react-query';

import { clearTrackStage, setTrackStage } from '@shared/acquisition/stageStore';

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

function invalidateLibrary(queryClient: QueryClient): void {
  void queryClient.invalidateQueries({ queryKey: ['library-home'] });
  void queryClient.invalidateQueries({ queryKey: ['library'] });
}

export function applyServerEvent(queryClient: QueryClient, event: ServerEvent): void {
  if (event.type === 'track_acquisition_progress') {
    const trackId = asString(event.data.track_id);
    const stage = asString(event.data.stage);
    // Ephemeral UI state in the stage store, NOT the query cache — a library
    // refetch must not be able to wipe it (that caused the pending→download→
    // pending flicker). No invalidation: progress events fire frequently.
    if (trackId && stage) {
      setTrackStage(trackId, stage);
    }
    return;
  }

  if (event.type === 'track_acquisition_completed') {
    const trackId = asString(event.data.track_id);
    if (trackId) {
      patchTrackInCaches(queryClient, trackId, {
        acquisition_status: 'ready',
        audio_ref: asString(event.data.audio_ref),
      });
      clearTrackStage(trackId);
    }
    invalidateLibrary(queryClient);
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
      clearTrackStage(trackId);
    }
    invalidateLibrary(queryClient);
    return;
  }

  const keys = INVALIDATION_MAP[event.type];
  if (!keys) return;
  for (const queryKey of keys) {
    void queryClient.invalidateQueries({ queryKey });
  }
}
