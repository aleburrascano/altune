import { useCallback, useEffect, useRef } from 'react';
import TrackPlayer from 'react-native-track-player';

import type { QueueStateResponse } from '@shared/api-client/playback';
import type { TrackResponse } from '@shared/api-client/types';
import { canPlay } from '@shared/playback/canPlay';
import { orderedQueueTracks, useQueueStore, type QueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';
import { currentSessionEpoch, isSameSession } from '@shared/session/signOutCleanup';

import { loadNativeQueue } from '@features/playback/native/loadNativeTrack';
import { withNativeQueue } from '@features/playback/native/nativeQueueLock';
import { activeNativeTrackId } from '@features/playback/native/nativeTrack';
import { playbackPort } from '@features/playback/ports/activePlaybackPort';
import { reportLoadFailure } from '@features/playback/hooks/usePlaybackErrorStore';
import {
  rebuildOnFirstWorkingRungReportingCurrent,
  showSavedTrackWhileRehydrating,
  type RungOutcome,
} from '@features/playback/domain/queueRebuildStrategies';
import {
  asRepeatMode,
  fromWireSource,
  parseQueueState,
  toWireSource,
} from '@features/playback/domain/queueStateWire';
import {
  recordQueueRestoreFailure,
  recordQueueSaveFailure,
  recordQueueStateRejected,
  type QueueRestoreStage,
} from '@features/playback/domain/playbackHealth';
import { warnPlayback } from '@features/playback/domain/redactPlaybackError';
import { progressSecondsToRoundedMs } from '@features/playback/domain/time';

import { useAppStateChange } from '@shared/lifecycle';

const SAVE_INTERVAL_MS = 15_000;
const MAX_SAVED_QUEUE_IDS = 10_000;

async function currentPositionMsOrZero(): Promise<number> {
  try {
    const progress = await TrackPlayer.getProgress();
    return progressSecondsToRoundedMs(progress.position);
  } catch {
    return 0;
  }
}

interface ConsistentSnapshot {
  state: QueueStore;
  positionMs: number;
}

function readConsistentSnapshot(): Promise<ConsistentSnapshot | null> {
  return withNativeQueue(async () => {
    const [activeKey, positionMs] = await Promise.all([
      activeNativeTrackId(),
      currentPositionMsOrZero(),
    ]);
    const state = useQueueStore.getState();
    const current = state.currentTrack();
    if (!current || activeKey !== trackKey(current)) return null;
    return { state, positionMs };
  }).catch(() => null);
}

function libraryIds(tracks: readonly PlaybackTrack[]): string[] {
  return tracks.map((t) => (t.source.kind === 'library' ? t.source.trackId : '')).filter(Boolean);
}

function savedCurrentIndex(s: QueueStore): number {
  const current = s.currentTrack();
  if (!current || current.source.kind !== 'library') return 0;
  const before = s.playOrder.slice(0, s.currentIndex);
  return libraryIds(orderedQueueTracks({ tracks: s.tracks, playOrder: before })).length;
}

interface SavedLists {
  track_ids: string[];
  natural_order: string[];
  current_index: number;
}

function windowStart(currentIndex: number, length: number): number {
  const centered = currentIndex - Math.floor(MAX_SAVED_QUEUE_IDS / 2);
  return Math.max(Math.min(centered, length - MAX_SAVED_QUEUE_IDS), 0);
}

function naturalWithin(natural: readonly string[], window: readonly string[]): string[] {
  const left = new Map<string, number>();
  for (const id of window) left.set(id, (left.get(id) ?? 0) + 1);
  return natural.filter((id) => {
    const count = left.get(id) ?? 0;
    left.set(id, count - 1);
    return count > 0;
  });
}

function windowOf(ordered: readonly string[], currentIndex: number) {
  const start = windowStart(currentIndex, ordered.length);
  return { ids: ordered.slice(start, start + MAX_SAVED_QUEUE_IDS), start };
}

function cappedSavedLists(s: QueueStore): SavedLists {
  const currentIndex = savedCurrentIndex(s);
  const { ids, start } = windowOf(libraryIds(orderedQueueTracks(s)), currentIndex);
  return {
    track_ids: ids,
    natural_order: naturalWithin(libraryIds(s.tracks), ids),
    current_index: currentIndex - start,
  };
}

type SaveFingerprint = readonly unknown[];

function fingerprintOf(s: QueueStore, positionMs: number): SaveFingerprint {
  return [s.tracks, s.playOrder, s.source, s.currentIndex, s.shuffled, s.repeatMode, positionMs];
}

function sameFingerprint(a: SaveFingerprint | null, b: SaveFingerprint): boolean {
  return a !== null && a.every((value, i) => Object.is(value, b[i]));
}

async function sendSave(s: QueueStore, positionMs: number): Promise<void> {
  await playbackPort().saveQueueState({
    ...cappedSavedLists(s),
    position_ms: positionMs,
    shuffled: s.shuffled,
    repeat_mode: s.repeatMode,
    source: toWireSource(s.source),
  });
}

async function readSaveSnapshot(
  isSkippable: (state: QueueStore) => boolean,
): Promise<ConsistentSnapshot | null> {
  if (isSkippable(useQueueStore.getState())) return null;
  const epoch = currentSessionEpoch();
  const snapshot = await readConsistentSnapshot();
  if (!snapshot || isSkippable(snapshot.state) || !isSameSession(epoch)) return null;
  return snapshot;
}

async function sendSaveReportingFailure(snapshot: ConsistentSnapshot): Promise<boolean> {
  try {
    await sendSave(snapshot.state, snapshot.positionMs);
    return true;
  } catch (err) {
    warnPlayback('failed to save queue state', {}, err);
    recordQueueSaveFailure();
    return false;
  }
}

async function saveOnce(
  isSkippable: (state: QueueStore) => boolean,
  lastSaved: { current: SaveFingerprint | null },
): Promise<void> {
  const snapshot = await readSaveSnapshot(isSkippable);
  if (!snapshot) return;
  const fingerprint = fingerprintOf(snapshot.state, snapshot.positionMs);
  if (sameFingerprint(lastSaved.current, fingerprint)) return;
  if (await sendSaveReportingFailure(snapshot)) lastSaved.current = fingerprint;
}

function userTookOver(owned: number): boolean {
  return useQueueStore.getState().generation !== owned;
}

function savedTracksMissingFrom(
  saved: QueueStateResponse,
  home: readonly TrackResponse[],
): boolean {
  const present = new Set<string>(home.map((t) => t.id));
  return [...saved.natural_order, ...saved.track_ids].some((id) => !present.has(id));
}

function warnOnSavedTracksMissingFromLibrary(
  saved: QueueStateResponse,
  trackMap: ReadonlyMap<string, TrackResponse>,
): void {
  const savedIds = new Set([...saved.natural_order, ...saved.track_ids]);
  const missing = [...savedIds].filter((id) => !trackMap.has(id));
  if (!missing.length) return;
  console.warn('[playback] saved queue tracks missing from the library read; restoring without', {
    missing: missing.length,
    saved: savedIds.size,
  });
}

function rebuildSavedQueue(saved: QueueStateResponse, home: readonly TrackResponse[]): RungOutcome {
  const trackMap = new Map<string, TrackResponse>(home.map((t) => [t.id, t]));
  const isReady = (id: string): boolean => canPlay(trackMap.get(id)?.acquisition_status);
  const source = fromWireSource(saved.source);
  warnOnSavedTracksMissingFromLibrary(saved, trackMap);

  return rebuildOnFirstWorkingRungReportingCurrent(saved, trackMap, isReady, source);
}

function applyRepeatMode(wireRepeatMode: string): void {
  const repeatMode = asRepeatMode(wireRepeatMode);
  if (repeatMode === 'all' || repeatMode === 'one') {
    useQueueStore.getState().setRepeatMode(repeatMode);
  }
}

async function resumeNativeQueue(positionMs: number): Promise<void> {
  const owned = useQueueStore.getState().generation;
  const s = useQueueStore.getState();
  if (!s.currentTrack() || userTookOver(owned)) return;

  await loadNativeQueue(orderedQueueTracks(s), s.currentIndex, {
    autoplay: false,
    startPositionMs: positionMs,
  });
}

function clearUnbackedPlaceholder(
  placeholderGeneration: number | null,
  stage: QueueRestoreStage,
): void {
  if (placeholderGeneration == null) return;
  if (useQueueStore.getState().generation !== placeholderGeneration) return;
  useQueueStore.getState().clearQueue();
  console.warn('[playback] cleared the unbacked resume placeholder', { stage });
}

function reportUnbackedRebuild(rebuiltGeneration: number | null, err: unknown): void {
  const queue = useQueueStore.getState();
  if (rebuiltGeneration == null || queue.generation !== rebuiltGeneration) return;
  const current = queue.currentTrack();
  if (current) reportLoadFailure(current, err);
}

async function restoreSavedQueue(
  markPlaceholderGeneration: (generation: number) => void,
): Promise<void> {
  let stage: QueueRestoreStage = 'fetch';
  let placeholderGeneration: number | null = null;
  let rebuiltGeneration: number | null = null;
  try {
    let owned = useQueueStore.getState().generation;

    const parsed = parseQueueState(await playbackPort().getQueueState());
    if (!parsed.ok) {
      console.warn(`[playback] rejected a malformed saved queue state: ${parsed.error.message}`);
      recordQueueStateRejected();
      return;
    }
    const saved = parsed.state;
    if (!saved.track_ids.length) return;
    if (userTookOver(owned)) return;

    stage = 'placeholder';
    placeholderGeneration = showSavedTrackWhileRehydrating(saved);
    if (placeholderGeneration != null) {
      owned = placeholderGeneration;
      markPlaceholderGeneration(placeholderGeneration);
    }

    stage = 'tracks';
    const { items: home, truncated } = await playbackPort().getAllTracks({});
    if (!home.length) return;
    if (userTookOver(owned)) return;
    if (truncated && savedTracksMissingFrom(saved, home)) {
      console.warn('[playback] library read hit its cap and lacks saved tracks; not restoring');
      return;
    }

    stage = 'rebuild';
    const rebuilt = rebuildSavedQueue(saved, home);
    if (rebuilt.rung === 'exhausted') return;
    const resumeMs = rebuilt.currentFound ? saved.position_ms : 0;
    useQueueStore.getState().setResumePosition(resumeMs);
    applyRepeatMode(saved.repeat_mode);
    rebuiltGeneration = useQueueStore.getState().generation;

    stage = 'native';
    await resumeNativeQueue(resumeMs);
  } catch (err) {
    warnPlayback('failed to restore the saved queue', { stage }, err);
    recordQueueRestoreFailure(stage);
    if (stage === 'native') reportUnbackedRebuild(rebuiltGeneration, err);
  } finally {
    clearUnbackedPlaceholder(placeholderGeneration, stage);
  }
}

interface SingleFlightState {
  inFlight: Promise<void> | null;
  dirty: boolean;
}

type SaveWork = () => Promise<void>;

async function drainSingleFlight(flight: SingleFlightState, work: SaveWork): Promise<void> {
  try {
    do {
      flight.dirty = false;
      await work();
    } while (flight.dirty);
  } finally {
    flight.inFlight = null;
  }
}

function requestSingleFlight(flight: SingleFlightState, work: () => Promise<void>): Promise<void> {
  if (flight.inFlight) {
    flight.dirty = true;
    return flight.inFlight;
  }
  const running = drainSingleFlight(flight, work);
  flight.inFlight = running;
  return running;
}

function createSingleFlight(work: () => Promise<void>): () => Promise<void> {
  const flight: SingleFlightState = { inFlight: null, dirty: false };
  return () => requestSingleFlight(flight, work);
}

export function useQueueResume() {
  const saveTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const restoredRef = useRef(false);
  const placeholderGenerationRef = useRef<number | null>(null);
  const singleFlightRef = useRef<(() => Promise<void>) | null>(null);
  const lastSavedRef = useRef<SaveFingerprint | null>(null);
  const save = useCallback((): Promise<void> => {
    const isSkippable = (state: QueueStore): boolean =>
      state.tracks.length === 0 ||
      placeholderGenerationRef.current === state.generation ||
      state.currentTrack()?.source.kind !== 'library';
    singleFlightRef.current ??= createSingleFlight(() => saveOnce(isSkippable, lastSavedRef));
    return singleFlightRef.current();
  }, []);

  useEffect(() => {
    if (restoredRef.current) return;
    restoredRef.current = true;

    void restoreSavedQueue((generation) => {
      placeholderGenerationRef.current = generation;
    });
  }, []);

  useEffect(() => {
    saveTimerRef.current = setInterval(() => {
      void save();
    }, SAVE_INTERVAL_MS);
    return () => {
      if (saveTimerRef.current) clearInterval(saveTimerRef.current);
    };
  }, [save]);

  useAppStateChange((state) => {
    if (state === 'background' || state === 'inactive') void save();
  });
}
