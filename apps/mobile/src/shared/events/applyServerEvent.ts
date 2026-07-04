/**
 * applyServerEvent — pure router from a server event to a cache/store effect.
 *
 * Acquisition events drive a single SSE-fed lifecycle store keyed by track_id
 * (the download store — membership AND phase in one place) AND patch the track's
 * acquisition_status across every cache (cache-as-truth, so every screen is
 * coherent at once), plus a library invalidate as a reconciliation backstop.
 * Membership/list events invalidate the affected lists. A `resync` control event
 * fully reconciles. Unknown types are ignored. Extracted from useServerEvents so
 * the routing is unit-testable without the SSE transport or AppState.
 */

import type { QueryClient } from '@tanstack/react-query';

import {
  startDownload,
  progressDownload,
  completeDownload,
  failDownload,
  type DownloadMeta,
  type DownloadPhase,
} from '@shared/acquisition/downloadStore';
import { stageToPhase } from '@shared/acquisition/stagePhase';
import type { TrackResponse } from '@shared/api-client/types';

import { getTrackFromCaches, patchTrackInCaches } from './trackCachePatch';
import type { ServerEvent } from './sse-client';

const INVALIDATION_MAP: Record<string, string[][]> = {
  track_added_to_library: [['library-home'], ['library']],
  track_deleted: [['library-home'], ['library'], ['playlists']],
  playlist_created: [['playlists']],
  playlist_deleted: [['playlists'], ['playlist']],
  track_added_to_playlist: [['playlist'], ['playlists']],
  track_removed_from_playlist: [['playlist'], ['playlists']],
};

// A resync control event (F4) means the server could not guarantee the client
// saw every event since its cursor (a replay gap after eviction, or a restart).
// The client cannot patch what it never received, so it fully reconciles every
// SSE-covered family.
const RESYNC_KEYS: string[][] = [['library-home'], ['library'], ['playlists'], ['playlist']];

function asString(value: unknown): string | null {
  return typeof value === 'string' ? value : null;
}

function invalidateLibrary(queryClient: QueryClient): void {
  void queryClient.invalidateQueries({ queryKey: ['library-home'] });
  void queryClient.invalidateQueries({ queryKey: ['library'] });
}

// Snapshot display metadata for the download store from whatever cache holds the
// track (the started/progress events carry only a track_id).
function trackMeta(track: TrackResponse | undefined): DownloadMeta | undefined {
  if (!track) return undefined;
  return { title: track.title, artist: track.artist, artworkUrl: track.artwork_url };
}

// A raw acquisition stage maps to a live download phase only for the three
// progress phases; anything else (an unknown/new stage) is ignored rather than
// stored as a fallback.
function progressPhase(stage: string | null): DownloadPhase | null {
  const phase = stageToPhase(stage);
  return phase === 'finding' || phase === 'downloading' || phase === 'finishing' ? phase : null;
}

export function applyServerEvent(queryClient: QueryClient, event: ServerEvent): void {
  if (event.type === 'resync') {
    for (const queryKey of RESYNC_KEYS) {
      void queryClient.invalidateQueries({ queryKey });
    }
    return;
  }

  if (event.type === 'track_acquisition_started') {
    const trackId = asString(event.data.track_id);
    if (trackId) {
      startDownload(trackId, trackMeta(getTrackFromCaches(queryClient, trackId)));
      // A re-acquired ready/failed track flips back to pending across caches so
      // every screen (and the library row's caption) reflects the restart.
      patchTrackInCaches(queryClient, trackId, {
        acquisition_status: 'pending',
        failure_reason: null,
      });
    }
    return;
  }

  if (event.type === 'track_acquisition_progress') {
    const trackId = asString(event.data.track_id);
    const phase = progressPhase(asString(event.data.stage));
    // Lifecycle store, NOT the query cache — a library refetch must not wipe it.
    // No invalidation: progress events fire frequently.
    if (trackId && phase) {
      progressDownload(trackId, phase, trackMeta(getTrackFromCaches(queryClient, trackId)));
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
      // Terminal sequence keeps the row mounted through finishing → done ✓; the
      // cache status flip no longer unmounts it (membership is store-driven).
      completeDownload(trackId);
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
      failDownload(trackId);
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
