import { useCallback, useEffect, useRef } from 'react';
import TrackPlayer from 'react-native-track-player';

import { useQueueStore, type QueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/playbackTrack';
import { currentSessionEpoch, isSameSession } from '@shared/session/signOutCleanup';

import { restoreSavedQueue } from '@features/playback/native/restoreSavedQueue';
import { withNativeQueue } from '@features/playback/native/nativeQueueLock';
import { activeNativeTrackId } from '@features/playback/native/nativeTrack';
import { playbackPort } from '@features/playback/ports/activePlaybackPort';
import { toWireSource } from '@features/playback/domain/queueStateWire';
import { recordQueueSaveFailure } from '@features/playback/domain/playbackHealth';
import { savedQueueLists } from '@features/playback/domain/queueSaveWindow';
import { warnPlayback } from '@features/playback/domain/playbackError';
import { progressSecondsToRoundedMs } from '@features/playback/domain/time';

import { useAppStateChange } from '@shared/lifecycle';

const SAVE_INTERVAL_MS = 15_000;

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

type SaveFingerprint = readonly unknown[];

function fingerprintOf(s: QueueStore, positionMs: number): SaveFingerprint {
  return [s.tracks, s.playOrder, s.source, s.currentIndex, s.shuffled, s.repeatMode, positionMs];
}

function sameFingerprint(a: SaveFingerprint | null, b: SaveFingerprint): boolean {
  return a !== null && a.every((value, i) => Object.is(value, b[i]));
}

async function sendSave(s: QueueStore, positionMs: number): Promise<void> {
  await playbackPort().saveQueueState({
    ...savedQueueLists(s),
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
