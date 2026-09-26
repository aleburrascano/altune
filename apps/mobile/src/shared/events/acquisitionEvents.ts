import type { QueryClient } from '@tanstack/react-query';

import {
  isTrackStatusReady,
  linkTrackIdentity,
  patchTrackStatus,
  trackIdentityKey,
} from '@shared/acquisition/trackStatusStore';
import {
  isStaleDownloadPhase,
  startDownload,
  progressDownload,
  completeDownload,
  failDownload,
  rememberDownloadMeta,
  type DownloadMeta,
  type DownloadPhase,
} from '@shared/acquisition/downloadStore';
import { invalidateAudioCaches } from '@shared/acquisition/audioCacheInvalidation';
import { stageToPhase } from '@shared/acquisition/stagePhase';
import { offlineDownloadsSupported } from '@shared/offline/offlineSupport';
import { repinIfPinned } from '@shared/offline/pinnedStore';
import { tryParseTrackResponse } from '@shared/api-client/tracks';
import type { TrackId } from '@shared/api-client/ids';
import {
  acquisitionOf,
  toFailed,
  toPending,
  toReady,
  toTrackStatus,
} from '@shared/api-client/trackAcquisition';
import type { TrackResponse } from '@shared/api-client/types';
import { libraryKeys, playlistKeys } from '@shared/lib/query-keys';

import { forgetTrack } from './forgetTrack';
import { asString, asTrackIdOrNull, type ServerEventHandlers } from './eventPayload';
import {
  getTrackFromCaches,
  invalidateLibraryDerived,
  scheduleTrackPatch,
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

// No default branch on purpose: the switch is exhaustive, so a new AcquisitionPhase
// leaves the end reachable and fails compilation (TS2366) until it is mapped here.
function progressPhase(stage: string | null): DownloadPhase | null {
  const phase = stageToPhase(stage);
  switch (phase) {
    case 'finding':
    case 'downloading':
    case 'finishing':
      return phase;
    case 'done':
    case 'failed':
    case 'working':
      return null;
  }
}

function handleTrackAddedToLibrary(queryClient: QueryClient, event: ServerEvent): void {
  const track = parseAddedTrack(event.data);
  invalidateLibraryDerived(queryClient);
  if (!track) {
    void queryClient.invalidateQueries({ queryKey: libraryKeys.tracksPrefix });
    void queryClient.invalidateQueries({ queryKey: libraryKeys.featuringPrefix });
    return;
  }
  upsertTrackInCaches(queryClient, track);
  patchTrackStatus(track.id, toTrackStatus(acquisitionOf(track)), 'sse');
  linkTrackIdentity(trackIdentityKey(track.title, track.artist), track.id);
  const meta = trackMeta(track);
  if (meta) rememberDownloadMeta(track.id, meta);
}

function handleTrackDeleted(queryClient: QueryClient, event: ServerEvent): void {
  const trackId = asTrackIdOrNull(event.data.track_id);
  if (trackId) {
    forgetTrack(queryClient, trackId);
  }
  invalidateLibraryDerived(queryClient);
  void queryClient.invalidateQueries({ queryKey: playlistKeys.list });
}

// A `started` replayed after the acquisition it announced already finished — an SSE reconnect,
// or a duplicate racing a real retry — would revert a ready track to "downloading" with no
// download behind it, and nothing short of another terminal event would put it back (#1784).
// A `failed` track is deliberately absent: a `started` is how a retry surfaces, and the
// download entry's own rank already absorbs a duplicate for as long as that attempt is shown.
function isStaleStart(trackId: TrackId): boolean {
  return isTrackStatusReady(trackId) || isStaleDownloadPhase(trackId, 'finding');
}

function handleTrackAcquisitionStarted(queryClient: QueryClient, event: ServerEvent): void {
  const trackId = asTrackIdOrNull(event.data.track_id);
  if (!trackId) return;
  if (isStaleStart(trackId)) return;
  const pending = toPending();
  startDownload(trackId, trackMeta(getTrackFromCaches(queryClient, trackId)));
  scheduleTrackPatch(queryClient, trackId, pending);
  patchTrackStatus(trackId, toTrackStatus(pending), 'sse');
}

function handleTrackAcquisitionProgress(queryClient: QueryClient, event: ServerEvent): void {
  const trackId = asTrackIdOrNull(event.data.track_id);
  const phase = progressPhase(asString(event.data.stage));
  if (trackId && phase) {
    progressDownload(trackId, phase, trackMeta(getTrackFromCaches(queryClient, trackId)));
  }
}

function handleTrackAcquisitionCompleted(queryClient: QueryClient, event: ServerEvent): void {
  const trackId = asTrackIdOrNull(event.data.track_id);
  if (!trackId) return;
  const ready = toReady();
  const audioRef = asString(event.data.audio_ref);
  scheduleTrackPatch(queryClient, trackId, {
    ...ready,
    ...(audioRef === null ? {} : { audio_ref: audioRef }),
  });
  patchTrackStatus(trackId, toTrackStatus(ready), 'sse');
  completeDownload(trackId);
  invalidateAudioCaches(trackId);
  if (offlineDownloadsSupported) repinIfPinned(trackId);
}

function handleTrackReplaceFailed(queryClient: QueryClient, event: ServerEvent): void {
  const trackId = asTrackIdOrNull(event.data.track_id);
  if (!trackId) return;
  const ready = toReady();
  scheduleTrackPatch(queryClient, trackId, ready);
  patchTrackStatus(trackId, toTrackStatus(ready), 'sse');
  failDownload(trackId);
}

function handleTrackAcquisitionFailed(queryClient: QueryClient, event: ServerEvent): void {
  const trackId = asTrackIdOrNull(event.data.track_id);
  if (!trackId) return;
  const failure = toFailed(asString(event.data.reason), asString(event.data.failure_message));
  // In the caches, an event without a message keeps the one already cached rather
  // than blanking it; the store keeps only what this event itself carried.
  const cachedMessage = getTrackFromCaches(queryClient, trackId)?.failure_message ?? null;
  scheduleTrackPatch(queryClient, trackId, {
    ...toFailed(failure.failure_reason, failure.failure_message ?? cachedMessage),
    audio_ref: null,
  });
  patchTrackStatus(trackId, toTrackStatus(failure), 'sse');
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
