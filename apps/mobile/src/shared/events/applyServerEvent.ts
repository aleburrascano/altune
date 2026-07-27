import type { QueryClient } from '@tanstack/react-query';

import {
  linkTrackIdentity,
  patchTrackStatus,
  removeTrackStatus,
  trackIdentityKey,
} from '@shared/acquisition/trackStatusStore';
import {
  startDownload,
  progressDownload,
  completeDownload,
  failDownload,
  type DownloadMeta,
  type DownloadPhase,
} from '@shared/acquisition/downloadStore';
import { invalidateAudioCaches } from '@shared/acquisition/audioCacheInvalidation';
import { stageToPhase } from '@shared/acquisition/stagePhase';
import { repinIfPinned } from '@shared/offline/pinnedStore';
import type { TrackResponse } from '@shared/api-client/types';
import { libraryKeys, playlistKeys } from '@shared/lib/query-keys';

import {
  patchPlaylistName,
  removeTrackFromPlaylistCache,
  reorderPlaylistCache,
} from './playlistCachePatch';
import {
  getTrackFromCaches,
  patchTrackInCaches,
  removeTrackFromCaches,
  upsertTrackInCaches,
} from './trackCachePatch';
import type { ServerEvent } from './sse-client';
import {
  isServerEventType,
  recordUnhandledEvent,
  type ServerEventType,
} from './eventTypes';

type InvalidateOnlyEvent = Exclude<
  ServerEventType,
  | 'resync'
  | 'track_added_to_library'
  | 'track_deleted'
  | 'track_acquisition_started'
  | 'track_acquisition_progress'
  | 'track_acquisition_completed'
  | 'track_acquisition_failed'
  | 'track_replace_failed'
  | 'playlist_renamed'
  | 'track_removed_from_playlist'
  | 'playlist_reordered'
>;

const INVALIDATION_MAP: Record<InvalidateOnlyEvent, readonly (readonly string[])[]> = {
  playlist_created: [playlistKeys.list],
  playlist_deleted: [playlistKeys.list, playlistKeys.details],
  track_added_to_playlist: [playlistKeys.details, playlistKeys.list],
};

const RESYNC_KEYS: readonly (readonly string[])[] = [
  libraryKeys.tracksPrefix,
  libraryKeys.lookupPrefix,
  libraryKeys.albumsPrefix,
  libraryKeys.artistsPrefix,
  libraryKeys.summary,
  libraryKeys.featuringPrefix,
  playlistKeys.list,
  playlistKeys.details,
];

function asString(value: unknown): string | null {
  return typeof value === 'string' ? value : null;
}

function asNumber(value: unknown): number | null {
  return typeof value === 'number' ? value : null;
}

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

function trackMeta(track: TrackResponse | undefined): DownloadMeta | undefined {
  if (!track) return undefined;
  return { title: track.title, artist: track.artist, artworkUrl: track.artwork_url };
}

function progressPhase(stage: string | null): DownloadPhase | null {
  const phase = stageToPhase(stage);
  return phase === 'finding' || phase === 'downloading' || phase === 'finishing' ? phase : null;
}

function invalidateDerived(queryClient: QueryClient): void {
  void queryClient.invalidateQueries({ queryKey: libraryKeys.albumsPrefix });
  void queryClient.invalidateQueries({ queryKey: libraryKeys.artistsPrefix });
  void queryClient.invalidateQueries({ queryKey: libraryKeys.summary });
}

export function applyServerEvent(queryClient: QueryClient, event: ServerEvent): void {
  if (!isServerEventType(event.type)) {
    recordUnhandledEvent(event.type);
    return;
  }
  route(queryClient, event, event.type);
}

function route(queryClient: QueryClient, event: ServerEvent, type: ServerEventType): void {
  if (type === 'resync') {
    for (const queryKey of RESYNC_KEYS) {
      void queryClient.invalidateQueries({ queryKey });
    }
    return;
  }

  if (type === 'track_added_to_library') {
    const track = parseAddedTrack(event.data);
    invalidateDerived(queryClient);
    if (track) {
      upsertTrackInCaches(queryClient, track);
      patchTrackStatus(track.id, {
        acquisitionStatus: track.acquisition_status,
        failureMessage: track.failure_message ?? null,
      });
      linkTrackIdentity(trackIdentityKey(track.title, track.artist), track.id);
    } else {
      void queryClient.invalidateQueries({ queryKey: libraryKeys.tracksPrefix });
      void queryClient.invalidateQueries({ queryKey: libraryKeys.featuringPrefix });
    }
    return;
  }

  if (type === 'track_deleted') {
    const trackId = asString(event.data.track_id);
    if (trackId) {
      removeTrackFromCaches(queryClient, trackId);
      removeTrackStatus(trackId);
    }
    invalidateDerived(queryClient);
    void queryClient.invalidateQueries({ queryKey: playlistKeys.list });
    return;
  }

  if (type === 'track_acquisition_started') {
    const trackId = asString(event.data.track_id);
    if (trackId) {
      startDownload(trackId, trackMeta(getTrackFromCaches(queryClient, trackId)));
      patchTrackInCaches(queryClient, trackId, {
        acquisition_status: 'pending',
        failure_reason: null,
        failure_message: null,
      });
      patchTrackStatus(trackId, { acquisitionStatus: 'pending', failureMessage: null });
    }
    return;
  }

  if (type === 'track_acquisition_progress') {
    const trackId = asString(event.data.track_id);
    const phase = progressPhase(asString(event.data.stage));
    if (trackId && phase) {
      progressDownload(trackId, phase, trackMeta(getTrackFromCaches(queryClient, trackId)));
    }
    return;
  }

  if (type === 'track_acquisition_completed') {
    const trackId = asString(event.data.track_id);
    if (trackId) {
      patchTrackInCaches(queryClient, trackId, {
        acquisition_status: 'ready',
        audio_ref: asString(event.data.audio_ref),
      });
      patchTrackStatus(trackId, { acquisitionStatus: 'ready', failureMessage: null });
      completeDownload(trackId);
      invalidateAudioCaches(trackId);
      repinIfPinned(trackId);
    }
    return;
  }

  if (type === 'track_replace_failed') {
    const trackId = asString(event.data.track_id);
    if (trackId) {
      patchTrackInCaches(queryClient, trackId, {
        acquisition_status: 'ready',
        failure_reason: null,
        failure_message: null,
      });
      patchTrackStatus(trackId, { acquisitionStatus: 'ready', failureMessage: null });
      failDownload(trackId);
    }
    return;
  }

  if (type === 'track_acquisition_failed') {
    const trackId = asString(event.data.track_id);
    if (trackId) {
      patchTrackInCaches(queryClient, trackId, {
        acquisition_status: 'failed',
        failure_reason: asString(event.data.reason),
        failure_message: asString(event.data.failure_message),
        audio_ref: null,
      });
      patchTrackStatus(trackId, {
        acquisitionStatus: 'failed',
        failureMessage: asString(event.data.failure_message),
      });
      failDownload(trackId);
    }
    return;
  }

  if (type === 'playlist_renamed') {
    const playlistId = asString(event.data.playlist_id);
    const name = asString(event.data.name);
    if (playlistId && name != null) patchPlaylistName(queryClient, playlistId, name);
    return;
  }

  if (type === 'track_removed_from_playlist') {
    const playlistId = asString(event.data.playlist_id);
    const trackId = asString(event.data.track_id);
    if (playlistId && trackId) removeTrackFromPlaylistCache(queryClient, playlistId, trackId);
    return;
  }

  if (type === 'playlist_reordered') {
    const playlistId = asString(event.data.playlist_id);
    const trackIds = Array.isArray(event.data.track_ids)
      ? event.data.track_ids.filter((v): v is string => typeof v === 'string')
      : null;
    if (playlistId && trackIds) reorderPlaylistCache(queryClient, playlistId, trackIds);
    return;
  }

  for (const queryKey of INVALIDATION_MAP[type]) {
    void queryClient.invalidateQueries({ queryKey });
  }
}
