import TrackPlayer from 'react-native-track-player';

import { useQueueStore, type QueueStore, trackKey } from '@shared/playback';
import { ApiError } from '@shared/failure';
import { reportClientError } from '@shared/telemetry';
import { currentSessionEpoch, isSameSession } from '@shared/session';

import { withNativeQueue } from '@features/player/native/nativeQueueLock';
import { activeNativeTrackId } from '@features/player/adapters/native/nativeTrack';
import { playbackPort } from '@features/player/ports/activePlaybackPort';
import type { IsSkippable } from '@features/player/ports/nativePlayback';
import { toWireSource } from '@features/player/domain/queueStateWire';
import { recordQueueSaveFailure } from '@features/player/domain/playbackHealth';
import { savedQueueLists } from '@features/player/domain/queueSaveWindow';
import { warnPlayback } from '@features/player/domain/playbackError';
import { progressSecondsToRoundedMs } from '@features/player/domain/time';

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
  }).catch((err: unknown) => {
    warnPlayback('failed to read queue snapshot', {}, err);
    return null;
  });
}

type SaveFingerprint = readonly [
  QueueStore['tracks'],
  QueueStore['playOrder'],
  QueueStore['source'],
  QueueStore['currentIndex'],
  QueueStore['shuffled'],
  QueueStore['repeatMode'],
  number,
];

function fingerprintOf(s: QueueStore, positionMs: number): SaveFingerprint {
  return [s.tracks, s.playOrder, s.source, s.currentIndex, s.shuffled, s.repeatMode, positionMs];
}

function sameFingerprint(a: SaveFingerprint | null, b: SaveFingerprint): boolean {
  return a !== null && a.length === b.length && a.every((value, i) => Object.is(value, b[i]));
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

async function readSaveSnapshot(isSkippable: IsSkippable): Promise<ConsistentSnapshot | null> {
  if (isSkippable(useQueueStore.getState())) return null;
  const epoch = currentSessionEpoch();
  const snapshot = await readConsistentSnapshot();
  if (!snapshot || isSkippable(snapshot.state) || !isSameSession(epoch)) return null;
  return snapshot;
}

const HTTP_BAD_REQUEST = 400;
const HTTP_UNPROCESSABLE = 422;
const VALIDATION_STATUSES: ReadonlySet<number> = new Set([HTTP_BAD_REQUEST, HTTP_UNPROCESSABLE]);

function isValidationRejection(err: unknown): err is ApiError {
  return err instanceof ApiError && err.code !== undefined && VALIDATION_STATUSES.has(err.status);
}

interface FingerprintRef {
  current: SaveFingerprint | null;
}

function isNewSighting(seen: FingerprintRef, queue: SaveFingerprint): boolean {
  if (sameFingerprint(seen.current, queue)) return false;
  seen.current = queue;
  return true;
}

function reportRejectedQueueOnce(err: unknown, state: QueueStore, reported: FingerprintRef): void {
  if (!isValidationRejection(err)) return;
  if (!isNewSighting(reported, fingerprintOf(state, 0))) return;
  reportClientError(new Error(`queue save rejected: ${err.code}`), 'playback');
}

function handleSaveFailure(err: unknown, state: QueueStore, reported: FingerprintRef): false {
  warnPlayback('failed to save queue state', {}, err);
  recordQueueSaveFailure();
  reportRejectedQueueOnce(err, state, reported);
  return false;
}

function sendSaveReportingFailure(s: ConsistentSnapshot, seen: FingerprintRef): Promise<boolean> {
  return sendSave(s.state, s.positionMs).then(
    () => true,
    (err: unknown) => handleSaveFailure(err, s.state, seen),
  );
}

async function saveOnce(
  isSkippable: IsSkippable,
  lastSaved: { current: SaveFingerprint | null },
  lastReported: FingerprintRef,
): Promise<void> {
  const snapshot = await readSaveSnapshot(isSkippable);
  if (!snapshot) return;
  const fingerprint = fingerprintOf(snapshot.state, snapshot.positionMs);
  if (sameFingerprint(lastSaved.current, fingerprint)) return;
  if (await sendSaveReportingFailure(snapshot, lastReported)) lastSaved.current = fingerprint;
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

export function createQueueSaver(isSkippable: IsSkippable): () => Promise<void> {
  const lastSaved: FingerprintRef = { current: null };
  const lastReported: FingerprintRef = { current: null };
  return createSingleFlight(() => saveOnce(isSkippable, lastSaved, lastReported));
}
