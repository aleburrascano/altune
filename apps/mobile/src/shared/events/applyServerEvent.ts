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

import {
  getTrackFromCaches,
  patchTrackInCaches,
  removeTrackFromCaches,
  upsertTrackInCaches,
} from './trackCachePatch';
import type { ServerEvent } from './sse-client';

const INVALIDATION_MAP: Record<string, string[][]> = {
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

function asNumber(value: unknown): number | null {
  return typeof value === 'number' ? value : null;
}

// Reconstructs a full TrackResponse from a track_added_to_library payload (F10).
// Returns null if the required identity/display fields are missing, so the
// caller can fall back to an invalidate for an older/thin payload.
function parseAddedTrack(data: Record<string, unknown>): TrackResponse | null {
  const id = asString(data.id) ?? asString(data.track_id);
  const title = asString(data.title);
  const artist = asString(data.artist);
  const addedAt = asString(data.added_at);
  const status = asString(data.acquisition_status);
  if (!id || !title || !artist || !addedAt || !status) return null;
  return {
    id,
    title,
    artist,
    album: asString(data.album),
    duration_seconds: asNumber(data.duration_seconds),
    added_at: addedAt,
    acquisition_status: status as TrackResponse['acquisition_status'],
    artwork_url: asString(data.artwork_url),
    failure_reason: asString(data.failure_reason),
    year: asNumber(data.year),
    genre: asString(data.genre),
    track_number: asNumber(data.track_number),
    album_artist: asString(data.album_artist),
    isrc: asString(data.isrc),
    audio_ref: asString(data.audio_ref),
  };
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

  if (event.type === 'track_added_to_library') {
    const track = parseAddedTrack(event.data);
    if (track) {
      upsertTrackInCaches(queryClient, track); // insert instantly, no refetch (F10)
    } else {
      // Older/thin payload (track_id only): fall back to a refetch.
      void queryClient.invalidateQueries({ queryKey: ['library-home'] });
      void queryClient.invalidateQueries({ queryKey: ['library'] });
    }
    return;
  }

  if (event.type === 'track_deleted') {
    const trackId = asString(event.data.track_id);
    if (trackId) {
      removeTrackFromCaches(queryClient, trackId); // drop the row everywhere (F11)
    }
    // The playlist summary track-counts can't be patched by id alone; a single
    // targeted refetch keeps them accurate without the two big library refetches.
    void queryClient.invalidateQueries({ queryKey: ['playlists'] });
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
    // No invalidate (F12): patchTrackInCaches already flipped every cache to
    // ready, and the detail save-control now reads the library reactively — so
    // the old 2000-row refetch on every finished download is gone.
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
    return;
  }

  const keys = INVALIDATION_MAP[event.type];
  if (!keys) return;
  for (const queryKey of keys) {
    void queryClient.invalidateQueries({ queryKey });
  }
}
