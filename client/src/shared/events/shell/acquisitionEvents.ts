import {
  isTrackStatusReady,
  linkTrackIdentity,
  patchTrackStatus,
  trackIdentityKey,
  isStaleDownloadPhase,
  startDownload,
  progressDownload,
  completeDownload,
  failDownload,
  rememberDownloadMeta,
  type DownloadMeta,
  type DownloadPhase,
  invalidateAudioCaches,
  stageToPhase,
} from '@shared/acquisition';
import { offlineDownloadsSupported, repinIfPinned } from '@shared/offline';
import { parseTrackResponseLenient } from '@shared/apiClient';
import { acquisitionOf, toFailed, toPending, toReady, toTrackStatus } from '@shared/catalog';
import type { TrackId, TrackResponse } from '@shared/catalog';
import type { JsonObject } from '@shared/files';
import { libraryKeys, playlistKeys } from '@shared/query';

import type { EventCache } from '@shared/events/core/eventCache';
import { asString, type ServerEventHandlers } from '@shared/events/core/serverEventTypes';
import type { ServerEvent } from '@shared/events/core/sseFrames';
import { asTrackIdOrNull } from '@shared/events/shell/eventIds';
import {
  forgetTrack,
  getTrackFromCaches,
  invalidateLibraryDerived,
  upsertTrackInCaches,
} from '@shared/events/shell/trackCachePatch';
import { scheduleTrackPatch, type TrackPatch } from '@shared/events/shell/trackPatchBatch';

type AcquisitionEventType =
  | 'track_added_to_library'
  | 'track_deleted'
  | 'track_acquisition_started'
  | 'track_acquisition_progress'
  | 'track_acquisition_completed'
  | 'track_acquisition_failed'
  | 'track_replace_failed';

function parseAddedTrack(eventData: Record<string, unknown>) {
  const payload =
    typeof eventData.id === 'string' ? eventData : { ...eventData, id: eventData.track_id };
  return parseTrackResponseLenient(payload);
}

function trackMeta(track: TrackResponse): DownloadMeta {
  return { title: track.title, artist: track.artist, artworkUrl: track.artwork_url };
}

function nonEmpty(text: string | null): string | null {
  return text === '' ? null : text;
}

function eventMeta(payload: JsonObject): DownloadMeta {
  return { title: nonEmpty(asString(payload.title)), artist: nonEmpty(asString(payload.artist)) };
}

function downloadMeta(cache: EventCache, trackId: TrackId, payload: JsonObject): DownloadMeta {
  const cached = getTrackFromCaches(cache, trackId);
  return cached ? trackMeta(cached) : eventMeta(payload);
}

const PROGRESS_PHASE: Record<ReturnType<typeof stageToPhase>, DownloadPhase | null> = {
  finding: 'finding',
  downloading: 'downloading',
  finishing: 'finishing',
  done: null,
  failed: null,
  working: null,
};

function progressPhase(stage: string | null): DownloadPhase | null {
  return PROGRESS_PHASE[stageToPhase(stage)];
}

function refetchTrackLists(cache: EventCache): void {
  void cache.invalidateQueries({ queryKey: libraryKeys.tracksPrefix });
  void cache.invalidateQueries({ queryKey: libraryKeys.featuringPrefix });
}

function addTrack(cache: EventCache, track: TrackResponse): void {
  upsertTrackInCaches(cache, track);
  patchTrackStatus(track.id, toTrackStatus(acquisitionOf(track)), 'sse');
  linkTrackIdentity(trackIdentityKey(track.title, track.artist), track.id);
  rememberDownloadMeta(track.id, trackMeta(track));
}

function handleTrackAddedToLibrary(cache: EventCache, event: ServerEvent): void {
  const parsed = parseAddedTrack(event.data);
  invalidateLibraryDerived(cache);
  if (parsed.ok) addTrack(cache, parsed.value);
  else refetchTrackLists(cache);
}

function handleTrackDeleted(cache: EventCache, event: ServerEvent): void {
  const trackId = asTrackIdOrNull(event.data.track_id);
  if (trackId) {
    forgetTrack(cache, trackId);
  }
  invalidateLibraryDerived(cache);
  void cache.invalidateQueries({ queryKey: playlistKeys.list });
}

function isStaleStart(trackId: TrackId): boolean {
  return isTrackStatusReady(trackId) || isStaleDownloadPhase(trackId, 'finding');
}

function handleTrackAcquisitionStarted(cache: EventCache, event: ServerEvent): void {
  const trackId = asTrackIdOrNull(event.data.track_id);
  if (!trackId) return;
  if (isStaleStart(trackId)) return;
  const pending = toPending();
  startDownload(trackId, downloadMeta(cache, trackId, event.data));
  scheduleTrackPatch(cache, trackId, pending);
  patchTrackStatus(trackId, toTrackStatus(pending), 'sse');
}

function handleTrackAcquisitionProgress(cache: EventCache, event: ServerEvent): void {
  const trackId = asTrackIdOrNull(event.data.track_id);
  const phase = progressPhase(asString(event.data.stage));
  if (trackId && phase) {
    progressDownload(trackId, phase, downloadMeta(cache, trackId, event.data));
  }
}

function readyPatch(audioRef: string | null): TrackPatch {
  const ready = toReady();
  return audioRef === null ? ready : { ...ready, audio_ref: audioRef };
}

function handleTrackAcquisitionCompleted(cache: EventCache, event: ServerEvent): void {
  const trackId = asTrackIdOrNull(event.data.track_id);
  if (!trackId) return;
  scheduleTrackPatch(cache, trackId, readyPatch(asString(event.data.audio_ref)));
  patchTrackStatus(trackId, toTrackStatus(toReady()), 'sse');
  completeDownload(trackId);
  invalidateAudioCaches(trackId);
  if (offlineDownloadsSupported) repinIfPinned(trackId);
}

function handleTrackReplaceFailed(cache: EventCache, event: ServerEvent): void {
  const trackId = asTrackIdOrNull(event.data.track_id);
  if (!trackId) return;
  const ready = toReady();
  scheduleTrackPatch(cache, trackId, ready);
  patchTrackStatus(trackId, toTrackStatus(ready), 'sse');
  failDownload(trackId);
}

function failedPatch(
  failure: ReturnType<typeof toFailed>,
  cachedMessage: string | null,
): TrackPatch {
  return {
    ...toFailed(failure.failure_reason, failure.failure_message ?? cachedMessage),
    audio_ref: null,
  };
}

function handleTrackAcquisitionFailed(cache: EventCache, event: ServerEvent): void {
  const trackId = asTrackIdOrNull(event.data.track_id);
  if (!trackId) return;
  const failure = toFailed(asString(event.data.reason), asString(event.data.failure_message));
  const cachedMessage = getTrackFromCaches(cache, trackId)?.failure_message ?? null;
  scheduleTrackPatch(cache, trackId, failedPatch(failure, cachedMessage));
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
