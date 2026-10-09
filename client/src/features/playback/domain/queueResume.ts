import type { QueueStateResponse } from '@shared/api-client/playback';
import type { TrackResponse } from '@shared/api-client/trackResponse';
import { canPlay } from '@shared/playback/canPlay';
import { useQueueStore } from '@shared/playback/queueStore';
import { currentTrackToPlaybackTrack, toPlaybackTrack } from '@shared/playback/toPlaybackTrack';
import type { QueueSource } from '@shared/playback/queueSource';

import {
  recordQueueRebuildOutcome,
  type QueueRebuildRung,
} from '@features/playback/domain/playbackHealth';
import { clamp } from '@features/playback/domain/clamp';
import { fromWireSource } from '@features/playback/domain/queueStateWire';

function savedCursor(savedTrackIds: readonly string[], savedCurrentIndex: number): number {
  return savedTrackIds[savedCurrentIndex] === undefined ? 0 : savedCurrentIndex;
}

export function currentTrackId(
  savedTrackIds: readonly string[],
  savedCurrentIndex: number,
): string {
  return savedTrackIds[savedCursor(savedTrackIds, savedCurrentIndex)] ?? '';
}

export function currentOccurrence(
  savedTrackIds: readonly string[],
  savedCurrentIndex: number,
): number {
  const cursor = savedCursor(savedTrackIds, savedCurrentIndex);
  const id = savedTrackIds[cursor];
  return savedTrackIds.slice(0, cursor).filter((other) => other === id).length;
}

function occurrenceIndex(ids: readonly string[], id: string, occurrence: number): number {
  let found = -1;
  let seen = 0;
  for (let i = 0; i < ids.length && seen <= occurrence; i++) {
    if (ids[i] !== id) continue;
    found = i;
    seen++;
  }
  return found;
}

function savedCurrentPosition(
  savedTrackIds: readonly string[],
  savedCurrentIndex: number,
  validTrackIds: readonly string[],
): number {
  const currentId = currentTrackId(savedTrackIds, savedCurrentIndex);
  const occurrence = currentOccurrence(savedTrackIds, savedCurrentIndex);
  return currentId ? occurrenceIndex(validTrackIds, currentId, occurrence) : -1;
}

export function isSavedCurrentResumable(
  savedTrackIds: readonly string[],
  savedCurrentIndex: number,
  validTrackIds: readonly string[],
): boolean {
  return savedCurrentPosition(savedTrackIds, savedCurrentIndex, validTrackIds) >= 0;
}

export function resolveResumeStartIndex(
  savedTrackIds: readonly string[],
  savedCurrentIndex: number,
  validTrackIds: readonly string[],
): number {
  if (validTrackIds.length === 0) return 0;
  const found = savedCurrentPosition(savedTrackIds, savedCurrentIndex, validTrackIds);
  if (found >= 0) return found;
  return clamp(savedCurrentIndex, 0, validTrackIds.length - 1);
}

function naturalPositions(naturalIds: readonly string[]): (id: string) => number | undefined {
  const positions = new Map<string, number[]>();
  naturalIds.forEach((id, i) => {
    const list = positions.get(id);
    if (list) list.push(i);
    else positions.set(id, [i]);
  });
  const taken = new Map<string, number>();
  return (id) => {
    const list = positions.get(id);
    if (!list) return undefined;
    const n = taken.get(id) ?? 0;
    taken.set(id, n + 1);
    return list[Math.min(n, list.length - 1)];
  };
}

export function reconstructPlayOrder(
  naturalIds: readonly string[],
  playIds: readonly string[],
  currentId: string,
  currentOccurrenceRank: number,
): { playOrder: number[]; currentIndex: number; found: boolean } {
  const nextNatural = naturalPositions(naturalIds);
  const playOrder: number[] = [];
  let currentIndex = 0;
  let currentSeen = 0;
  let found = false;
  for (const id of playIds) {
    const ni = nextNatural(id);
    if (ni === undefined) continue;
    if (id === currentId && currentSeen++ <= currentOccurrenceRank) {
      currentIndex = playOrder.length;
      found = true;
    }
    playOrder.push(ni);
  }
  return { playOrder, currentIndex, found };
}

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
