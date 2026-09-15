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

import { asString, type ServerEventHandlers } from './eventPayload';
import {
  getTrackFromCaches,
  patchTrackInCaches,
  removeTrackFromCaches,
  upsertTrackInCaches,
} from './trackCachePatch';
import type { ServerEvent } from './sse-client';

type AcquisitionEventType =
  | 'track_added_to_library'
  | 'track_deleted'
  | 'track_acquisition_started'
  | 'track_acquisition_progress'
  | 'track_acquisition_completed'
  | 'track_acquisition_failed'
  | 'track_replace_failed';

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

export const ACQUISITION_HANDLERS: ServerEventHandlers<AcquisitionEventType> = {
  track_added_to_library: handleTrackAddedToLibrary,
  track_deleted: handleTrackDeleted,
  track_acquisition_started: handleTrackAcquisitionStarted,
  track_acquisition_progress: handleTrackAcquisitionProgress,
  track_acquisition_completed: handleTrackAcquisitionCompleted,
  track_acquisition_failed: handleTrackAcquisitionFailed,
  track_replace_failed: handleTrackReplaceFailed,
};
