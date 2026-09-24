import { useCallback, useEffect, useRef } from 'react';
import TrackPlayer from 'react-native-track-player';

import type { QueueStateResponse } from '@shared/api-client/playback';
import { getQueueState, saveQueueState } from '@shared/api-client/playback';
import { getAllTracks } from '@shared/api-client/tracks';
import type { TrackResponse } from '@shared/api-client/types';
import { canPlay } from '@shared/playback/canPlay';
import { orderedQueueTracks, useQueueStore, type QueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import { loadNativeQueue } from '../loadNativeTrack';
import { withNativeQueue } from '../nativeQueueLock';
import { activeNativeTrackId } from '../nativeTrack';
import {
  rebuildOnFirstWorkingRung,
  showSavedTrackWhileRehydrating,
} from '../queueRebuildStrategies';
import { asRepeatMode, fromWireSource, parseQueueState, toWireSource } from '../queueStateWire';

import { useAppStateChange } from './useAppStateChange';

const SAVE_INTERVAL_MS = 15_000;

async function currentPositionMsOrZero(): Promise<number> {
  try {
    const progress = await TrackPlayer.getProgress();
    return Math.round(progress.position * 1000);
  } catch {
    return 0;
  }
}

interface ConsistentSnapshot {
  state: QueueStore;
  positionMs: number;
}

// Reads the native position and the queue snapshot as one consistent pair, or null
// when they disagree. Runs inside withNativeQueue so no load/skip op is mid-flight
// between the native reads, and requires the native active item to be the store's
// current track (nativeTrack ids are trackKey) — a load that has not yet reset, is
// waiting on its URL round trip, or has not reached its start index fails the check,
// so a save never pairs the new queue with the previous track's position.
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

// The current track's index within the saved library ids, counted by queue position
// rather than looked up by id: the same track can sit in the queue more than once.
function savedCurrentIndex(s: QueueStore): number {
  const current = s.currentTrack();
  if (!current || current.source.kind !== 'library') return 0;
  const before = s.playOrder.slice(0, s.currentIndex);
  return libraryIds(orderedQueueTracks({ tracks: s.tracks, playOrder: before })).length;
}

// One save: a consistent snapshot, then its PUT. `isSkippable` rejects an empty queue
// or the rehydration placeholder, checked before and after waiting on the native lock.
async function saveOnce(isSkippable: (state: QueueStore) => boolean): Promise<void> {
  if (isSkippable(useQueueStore.getState())) return;

  const snapshot = await readConsistentSnapshot();
  if (!snapshot || isSkippable(snapshot.state)) return;
  const { state: s, positionMs } = snapshot;

  try {
    await saveQueueState({
      track_ids: libraryIds(orderedQueueTracks(s)),
      current_index: savedCurrentIndex(s),
      position_ms: positionMs,
      shuffled: s.shuffled,
      repeat_mode: s.repeatMode,
      source: toWireSource(s.source),
      natural_order: libraryIds(s.tracks),
    });
  } catch (err) {
    console.warn('[playback] failed to save queue state', { error: err });
  }
}

// Every step of a restore owns the generation it started from: the user can load their
// own queue mid-restore, and the restore must then leave it alone.
function userTookOver(owned: number): boolean {
  return useQueueStore.getState().generation !== owned;
}

// Saved ids the library read never returned are dropped from the rebuilt queue, so the
// user gets a shorter queue than they left. Past getAllTracks' own cap that truncation is
// invisible from the queue's side, and this line is where it shows (#1740).
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

function rebuildSavedQueue(saved: QueueStateResponse, home: readonly TrackResponse[]): boolean {
  const trackMap = new Map<string, TrackResponse>(home.map((t) => [t.id, t]));
  const isReady = (id: string): boolean => canPlay(trackMap.get(id)?.acquisition_status);
  const source = fromWireSource(saved.source);
  warnOnSavedTracksMissingFromLibrary(saved, trackMap);

  return rebuildOnFirstWorkingRung(saved, trackMap, isReady, source) !== 'exhausted';
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

// The restore chain is several steps deep, so a bare "restore failed" line cannot tell a
// "my queue never resumes" report apart from a dead network (#1743): the stage names the
// step that threw.
type RestoreStage = 'fetch' | 'placeholder' | 'tracks' | 'rebuild' | 'native';

// The placeholder is a "now playing" card with nothing loaded in the native player, so a
// restore that never reaches the native load has to take it back down (#1726): play, pause
// and seek are no-ops against it. Still holding the placeholder's generation means nothing
// has replaced it — a later generation is a rebuilt queue or the user's own, and that queue
// is what is on screen.
function clearUnbackedPlaceholder(placeholderGeneration: number | null, stage: RestoreStage): void {
  if (placeholderGeneration == null) return;
  if (useQueueStore.getState().generation !== placeholderGeneration) return;
  useQueueStore.getState().clearQueue();
  console.warn('[playback] cleared the unbacked resume placeholder', { stage });
}

// One restore, from the saved row to the native queue. `markPlaceholderGeneration` hands
// the rehydration placeholder's generation to the save path, which skips saving that
// generation back.
async function restoreSavedQueue(
  markPlaceholderGeneration: (generation: number) => void,
): Promise<void> {
  let stage: RestoreStage = 'fetch';
  let placeholderGeneration: number | null = null;
  try {
    let owned = useQueueStore.getState().generation;

    const parsed = parseQueueState(await getQueueState());
    if (!parsed.ok) {
      console.warn(`[playback] rejected a malformed saved queue state: ${parsed.error.message}`);
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
    const home = await getAllTracks({});
    if (!home.length) return;
    if (userTookOver(owned)) return;

    stage = 'rebuild';
    if (!rebuildSavedQueue(saved, home)) return;
    useQueueStore.getState().setResumePosition(saved.position_ms);
    applyRepeatMode(saved.repeat_mode);

    stage = 'native';
    await resumeNativeQueue(saved.position_ms);
  } catch (err) {
    console.warn('[playback] failed to restore the saved queue', { stage, error: err });
  } finally {
    clearUnbackedPlaceholder(placeholderGeneration, stage);
  }
}

export function useQueueResume() {
  const saveTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const restoredRef = useRef(false);
  const placeholderGenerationRef = useRef<number | null>(null);
  const saveInFlightRef = useRef<Promise<void> | null>(null);
  const saveAgainRef = useRef(false);

  // The interval and AppState triggers can fire together. Each save's snapshot is
  // read when it starts, so letting two PUTs overlap lets the older one land last.
  // Saves are single-flight: a trigger during an in-flight save only marks it dirty,
  // and one follow-up save then reads a fresh snapshot after the PUT has settled.
  const save = useCallback((): Promise<void> => {
    if (saveInFlightRef.current) {
      saveAgainRef.current = true;
      return saveInFlightRef.current;
    }
    const isSkippable = (state: QueueStore): boolean =>
      state.tracks.length === 0 || placeholderGenerationRef.current === state.generation;
    const run = async (): Promise<void> => {
      try {
        do {
          saveAgainRef.current = false;
          await saveOnce(isSkippable);
        } while (saveAgainRef.current);
      } finally {
        saveInFlightRef.current = null;
      }
    };
    saveInFlightRef.current = run();
    return saveInFlightRef.current;
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
