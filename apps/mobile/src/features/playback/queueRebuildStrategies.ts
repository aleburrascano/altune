import type { QueueStateResponse } from '@shared/api-client/playback';
import type { TrackResponse } from '@shared/api-client/types';
import { canPlay } from '@shared/playback/canPlay';
import { useQueueStore } from '@shared/playback/queueStore';
import { currentTrackToPlaybackTrack, toPlaybackTrack } from '@shared/playback/toPlaybackTrack';
import type { QueueSource } from '@shared/playback/types';

import { recordQueueRebuildOutcome, type QueueRebuildRung } from './playbackHealth';
import { fromWireSource } from './queueStateWire';
import {
  currentOccurrence,
  currentTrackId,
  isSavedCurrentResumable,
  reconstructPlayOrder,
  resolveResumeStartIndex,
} from './resumeQueue';

export function showSavedTrackWhileRehydrating(saved: QueueStateResponse): number | null {
  if (!saved.current_track || !canPlay(saved.current_track.acquisition_status)) return null;

  const current = currentTrackToPlaybackTrack(saved.current_track);
  useQueueStore.getState().loadQueue([current], 0, fromWireSource(saved.source));
  useQueueStore.getState().setResumePosition(saved.position_ms);
  return useQueueStore.getState().generation;
}

export interface RungOutcome {
  rung: QueueRebuildRung;
  currentFound: boolean;
}

export function rebuildOnFirstWorkingRungReportingCurrent(
  saved: QueueStateResponse,
  trackMap: Map<string, TrackResponse>,
  isReady: (id: string) => boolean,
  source: QueueSource | null,
): RungOutcome {
  const natural = attemptNaturalOrder(saved, trackMap, isReady, source);
  const playOrder = natural ? null : attemptPlayOrderAlone(saved, trackMap, source);
  return recordedOutcome(natural, playOrder);
}

function recordedRung(rung: QueueRebuildRung): QueueRebuildRung {
  recordQueueRebuildOutcome(rung);
  return rung;
}

function recordedOutcome(natural: Attempt | null, playOrder: Attempt | null): RungOutcome {
  const rung = natural ? 'natural' : playOrder ? 'play_order' : 'exhausted';
  const found = (natural ?? playOrder)?.currentFound ?? false;
  return { rung: recordedRung(rung), currentFound: found };
}

interface Attempt {
  currentFound: boolean;
}

function attemptNaturalOrder(
  saved: QueueStateResponse,
  trackMap: Map<string, TrackResponse>,
  isReady: (id: string) => boolean,
  source: QueueSource | null,
): Attempt | null {
  const rebuilt = reconstructNatural(saved, isReady);
  return rebuilt ? restoreNatural(trackMap, source, rebuilt) : null;
}

type NaturalRebuild = ReturnType<typeof reconstructPlayOrder> & {
  naturalIds: string[];
  shuffled: boolean;
};

function reconstructNatural(
  saved: QueueStateResponse,
  isReady: (id: string) => boolean,
): NaturalRebuild | null {
  const naturalIds = saved.natural_order.filter(isReady);
  const playIds = saved.track_ids.filter(isReady);
  const rebuilt = reconstructPlayOrder(naturalIds, playIds, ...savedCurrent(saved));
  if (!naturalIds.length || !rebuilt.playOrder.length) return null;
  return { ...rebuilt, naturalIds, shuffled: saved.shuffled };
}

function savedCurrent(saved: QueueStateResponse): [string, number] {
  const { track_ids: ids, current_index: at } = saved;
  return [currentTrackId(ids, at), currentOccurrence(ids, at)];
}

function restoreNatural(
  trackMap: Map<string, TrackResponse>,
  source: QueueSource | null,
  { naturalIds, found, ...queue }: NaturalRebuild,
): Attempt {
  const tracks = naturalIds.map((id) => toPlaybackTrack(trackMap.get(id)!));
  useQueueStore.getState().restoreQueue({ ...queue, tracks, source });
  return { currentFound: found };
}

function attemptPlayOrderAlone(
  saved: QueueStateResponse,
  trackMap: Map<string, TrackResponse>,
  source: QueueSource | null,
): Attempt | null {
  const validTracks = playableTracks(saved, trackMap);
  if (!validTracks.length) return null;
  loadPlayOrder(saved, validTracks, source);
  return { currentFound: isSavedCurrentResumable(...savedIdsAt(saved), validIds(validTracks)) };
}

function playableTracks(
  saved: QueueStateResponse,
  trackMap: Map<string, TrackResponse>,
): TrackResponse[] {
  return saved.track_ids
    .map((id) => trackMap.get(id))
    .filter((t): t is TrackResponse => t != null && canPlay(t.acquisition_status));
}

function validIds(tracks: TrackResponse[]): string[] {
  return tracks.map((t) => t.id);
}

function savedIdsAt(saved: QueueStateResponse): [string[], number] {
  return [saved.track_ids, saved.current_index];
}

function loadPlayOrder(
  saved: QueueStateResponse,
  validTracks: TrackResponse[],
  source: QueueSource | null,
): void {
  const startIdx = resolveResumeStartIndex(...savedIdsAt(saved), validIds(validTracks));
  useQueueStore.getState().loadQueue(validTracks.map(toPlaybackTrack), startIdx, source);
  if (saved.shuffled) useQueueStore.getState().setShuffled(true);
}
