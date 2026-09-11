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
import { tryParseTrackResponse } from '@shared/api-client/parse';
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
import { isServerEventType, recordUnhandledEvent, type ServerEventType } from './eventTypes';

type Handler = (queryClient: QueryClient, event: ServerEvent) => void;

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

// The SSE `track_added_to_library` payload carries the id under `id`, or `track_id`
// on older servers; normalise before handing it to the shared TrackResponse parser.
function parseAddedTrack(data: Record<string, unknown>): TrackResponse | null {
  const payload = typeof data.id === 'string' ? data : { ...data, id: data.track_id };
  return tryParseTrackResponse(payload);
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

function stringArray(value: unknown): string[] | null {
  return Array.isArray(value) ? value.filter((v): v is string => typeof v === 'string') : null;
}

function invalidateKeys(keys: readonly (readonly string[])[]): Handler {
  return (queryClient) => {
    for (const queryKey of keys) {
      void queryClient.invalidateQueries({ queryKey });
    }
  };
}

function handleResync(queryClient: QueryClient): void {
  for (const queryKey of RESYNC_KEYS) {
    void queryClient.invalidateQueries({ queryKey });
  }
}

function handleTrackAddedToLibrary(queryClient: QueryClient, event: ServerEvent): void {
  const track = parseAddedTrack(event.data);
  invalidateDerived(queryClient);
  if (!track) {
    void queryClient.invalidateQueries({ queryKey: libraryKeys.tracksPrefix });
    void queryClient.invalidateQueries({ queryKey: libraryKeys.featuringPrefix });
    return;
  }
  upsertTrackInCaches(queryClient, track);
  patchTrackStatus(track.id, {
    acquisitionStatus: track.acquisition_status,
    failureMessage: track.failure_message ?? null,
  });
  linkTrackIdentity(trackIdentityKey(track.title, track.artist), track.id);
}

function handleTrackDeleted(queryClient: QueryClient, event: ServerEvent): void {
  const trackId = asString(event.data.track_id);
  if (trackId) {
    removeTrackFromCaches(queryClient, trackId);
    removeTrackStatus(trackId);
  }
  invalidateDerived(queryClient);
  void queryClient.invalidateQueries({ queryKey: playlistKeys.list });
}

function handleTrackAcquisitionStarted(queryClient: QueryClient, event: ServerEvent): void {
  const trackId = asString(event.data.track_id);
  if (!trackId) return;
  startDownload(trackId, trackMeta(getTrackFromCaches(queryClient, trackId)));
  patchTrackInCaches(queryClient, trackId, {
    acquisition_status: 'pending',
    failure_reason: null,
    failure_message: null,
  });
  patchTrackStatus(trackId, { acquisitionStatus: 'pending', failureMessage: null });
}

function handleTrackAcquisitionProgress(queryClient: QueryClient, event: ServerEvent): void {
  const trackId = asString(event.data.track_id);
  const phase = progressPhase(asString(event.data.stage));
  if (trackId && phase) {
    progressDownload(trackId, phase, trackMeta(getTrackFromCaches(queryClient, trackId)));
  }
}

function handleTrackAcquisitionCompleted(queryClient: QueryClient, event: ServerEvent): void {
  const trackId = asString(event.data.track_id);
  if (!trackId) return;
  const audioRef = asString(event.data.audio_ref);
  patchTrackInCaches(queryClient, trackId, {
    acquisition_status: 'ready',
    ...(audioRef === null ? {} : { audio_ref: audioRef }),
  });
  patchTrackStatus(trackId, { acquisitionStatus: 'ready', failureMessage: null });
  completeDownload(trackId);
  invalidateAudioCaches(trackId);
  repinIfPinned(trackId);
}

function handleTrackReplaceFailed(queryClient: QueryClient, event: ServerEvent): void {
  const trackId = asString(event.data.track_id);
  if (!trackId) return;
  patchTrackInCaches(queryClient, trackId, {
    acquisition_status: 'ready',
    failure_reason: null,
    failure_message: null,
  });
  patchTrackStatus(trackId, { acquisitionStatus: 'ready', failureMessage: null });
  failDownload(trackId);
}

function handleTrackAcquisitionFailed(queryClient: QueryClient, event: ServerEvent): void {
  const trackId = asString(event.data.track_id);
  if (!trackId) return;
  const failureMessage = asString(event.data.failure_message);
  patchTrackInCaches(queryClient, trackId, {
    acquisition_status: 'failed',
    failure_reason: asString(event.data.reason),
    ...(failureMessage === null ? {} : { failure_message: failureMessage }),
    audio_ref: null,
  });
  patchTrackStatus(trackId, { acquisitionStatus: 'failed', failureMessage });
  failDownload(trackId);
}

function handlePlaylistRenamed(queryClient: QueryClient, event: ServerEvent): void {
  const playlistId = asString(event.data.playlist_id);
  const name = asString(event.data.name);
  if (playlistId && name != null) patchPlaylistName(queryClient, playlistId, name);
}

function handleTrackRemovedFromPlaylist(queryClient: QueryClient, event: ServerEvent): void {
  const playlistId = asString(event.data.playlist_id);
  const trackId = asString(event.data.track_id);
  if (playlistId && trackId) removeTrackFromPlaylistCache(queryClient, playlistId, trackId);
}

function handleTracksRemovedFromPlaylist(queryClient: QueryClient, event: ServerEvent): void {
  const playlistId = asString(event.data.playlist_id);
  const trackIds = stringArray(event.data.track_ids);
  if (!playlistId || !trackIds) return;
  for (const trackId of trackIds) {
    removeTrackFromPlaylistCache(queryClient, playlistId, trackId);
  }
}

function handlePlaylistReordered(queryClient: QueryClient, event: ServerEvent): void {
  const playlistId = asString(event.data.playlist_id);
  const trackIds = stringArray(event.data.track_ids);
  if (playlistId && trackIds) reorderPlaylistCache(queryClient, playlistId, trackIds);
}

const HANDLERS: Record<ServerEventType, Handler> = {
  resync: handleResync,
  track_added_to_library: handleTrackAddedToLibrary,
  track_deleted: handleTrackDeleted,
  track_acquisition_started: handleTrackAcquisitionStarted,
  track_acquisition_progress: handleTrackAcquisitionProgress,
  track_acquisition_completed: handleTrackAcquisitionCompleted,
  track_acquisition_failed: handleTrackAcquisitionFailed,
  track_replace_failed: handleTrackReplaceFailed,
  track_added_to_playlist: invalidateKeys([playlistKeys.details, playlistKeys.list]),
  tracks_added_to_playlist: invalidateKeys([playlistKeys.details, playlistKeys.list]),
  track_removed_from_playlist: handleTrackRemovedFromPlaylist,
  tracks_removed_from_playlist: handleTracksRemovedFromPlaylist,
  playlist_created: invalidateKeys([playlistKeys.list]),
  playlist_deleted: invalidateKeys([playlistKeys.list, playlistKeys.details]),
  playlist_renamed: handlePlaylistRenamed,
  playlist_reordered: handlePlaylistReordered,
};

export function applyServerEvent(queryClient: QueryClient, event: ServerEvent): void {
  if (!isServerEventType(event.type)) {
    recordUnhandledEvent(event.type);
    return;
  }
  HANDLERS[event.type](queryClient, event);
}
