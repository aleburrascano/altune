import TrackPlayer, {
  Event,
  type PlaybackErrorEvent,
  type RemoteDuckEvent,
  type RemoteSeekEvent,
} from 'react-native-track-player';

import { shouldRestartOnPrevious } from '@shared/playback/constants';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import { type TrackKey, trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import { registerAudioCacheInvalidator } from '@shared/acquisition/audioCacheInvalidation';
import { recoverAudio } from '@shared/api-client/audio';
import { parseTrackId } from '@shared/api-client/ids';
import { hasSignedInUser } from '@shared/session/signOutCleanup';
import { discardPrefetchedAudio, evictCached, prefetchNext } from './audioPrefetch';
import { refreshUpcomingPresign } from './loadNativeTrack';
import { claimSessionReset } from '../loadToken';
import { withNativeQueue } from './nativeQueueLock';
import { shouldApplyActiveIndex } from './nativeSyncGuard';
import { activeNativeTrackId } from './nativeTrack';
import { forgetAllSwaps, repairActiveToStreaming, wasSwappedToLocal } from './nativeTrackSwap';
import { classifyNativePlaybackError } from '../classifyPlaybackError';
import { clearPlaybackError, reportPlaybackError } from '../playbackErrorStore';
import { recordPlaybackFailure } from '../playbackHealth';
import { reportingQueueFailure, reportQueueFailure } from '../queueFailureReport';

export async function resetPlaybackForSignOut(): Promise<void> {
  claimSessionReset();
  useQueueStore.getState().clearQueue();
  clearPlaybackError();
  recoveryRuns.clear();
  discardPrefetchedAudio();
  await resetNativeQueueForSignOut();
}

function resetNativeQueue(): Promise<void> {
  return withNativeQueue(async () => {
    await TrackPlayer.reset();
    forgetAllSwaps();
  });
}

async function resetNativeQueueForSignOut(): Promise<void> {
  try {
    await resetNativeQueue();
  } catch (err) {
    reportQueueFailure(null, 'signOutReset', err);
    await retryNativeQueueReset();
  }
}

async function retryNativeQueueReset(): Promise<void> {
  try {
    await resetNativeQueue();
  } catch (err) {
    reportQueueFailure(null, 'signOutResetRetry', err);
    throw err;
  }
}

function whenSignedIn<Args extends unknown[]>(handler: (...args: Args) => void) {
  return (...args: Args): void => {
    if (hasSignedInUser()) handler(...args);
  };
}

function queueTrackByKey(key: TrackKey): PlaybackTrack | null {
  const s = useQueueStore.getState();
  return orderedQueueTracks(s).find((t) => trackKey(t) === key) ?? null;
}

function currentQueueTrackKey(): TrackKey | null {
  const current = useQueueStore.getState().currentTrack();
  return current === null ? null : trackKey(current);
}

function slidePresignWindow(index: number): Promise<void> {
  return reportingQueueFailure(currentQueueTrackKey, 'refreshUpcomingPresign', () =>
    refreshUpcomingPresign(index),
  );
}

export const RECOVERY_ATTEMPTS_PER_TRACK = 2;

export const RECOVERY_COOLDOWN_BASE_MS = 30_000;

const RECOVERY_MEMORY_MS = 10 * 60_000;

export const MAX_TRACKED_RECOVERIES = 64;

type RecoveryRun = { attempts: number; lastAttemptAt: number };

const recoveryRuns = new Map<TrackKey, RecoveryRun>();

function hasSettled(run: RecoveryRun, now: number): boolean {
  const elapsed = now - run.lastAttemptAt;
  return elapsed < 0 || elapsed >= RECOVERY_MEMORY_MS;
}

function cooldownMs(attempts: number): number {
  if (attempts < RECOVERY_ATTEMPTS_PER_TRACK) return 0;
  const doubledPerExtraAttempt =
    RECOVERY_COOLDOWN_BASE_MS * 2 ** (attempts - RECOVERY_ATTEMPTS_PER_TRACK);
  return Math.min(doubledPerExtraAttempt, RECOVERY_MEMORY_MS);
}

function isCoolingDown(run: RecoveryRun, now: number): boolean {
  return now - run.lastAttemptAt < cooldownMs(run.attempts);
}

function forgetSettledRuns(now: number): void {
  for (const [key, run] of recoveryRuns) {
    if (hasSettled(run, now)) recoveryRuns.delete(key);
  }
}

function claimRecoveryAttempt(key: TrackKey, now: number): boolean {
  forgetSettledRuns(now);
  const run = recoveryRuns.get(key);
  if (run !== undefined && isCoolingDown(run, now)) return false;
  if (run === undefined && recoveryRuns.size >= MAX_TRACKED_RECOVERIES) return false;
  recoveryRuns.set(key, { attempts: (run?.attempts ?? 0) + 1, lastAttemptAt: now });
  return true;
}

async function handlePlaybackError({ code, message }: PlaybackErrorEvent): Promise<void> {
  const key = (await activeNativeTrackId()) ?? null;
  const failed = key !== null ? queueTrackByKey(key) : useQueueStore.getState().currentTrack();
  const failedKey = key ?? (failed ? trackKey(failed) : null);
  const kind = classifyNativePlaybackError(code ?? '', message ?? '');
  recordPlaybackFailure(kind);
  if (failedKey !== null) reportPlaybackError(failedKey, kind, message || 'Playback failed');

  if (!failed || failed.source.kind !== 'library') return;
  if (!claimRecoveryAttempt(trackKey(failed), Date.now())) return;
  if (wasSwappedToLocal(failed.source.trackId)) {
    await repairActiveToStreaming(failed);
    return;
  }
  await recoverAudio(failed.source.trackId).catch(() => {});
}

function handleRemoteDuck(data: RemoteDuckEvent): void {
  if (data.permanent) return;
  if (data.paused) {
    void TrackPlayer.pause();
    return;
  }
  if (!hasSignedInUser()) return;
  void TrackPlayer.play();
}

async function playPreviousRemotely(): Promise<void> {
  const { position } = await TrackPlayer.getProgress();
  if (shouldRestartOnPrevious(position * 1000)) {
    await TrackPlayer.seekTo(0);
    return;
  }
  await withNativeQueue(() => TrackPlayer.skipToPrevious());
}

function evictCachedIfParsable(trackId: string): void {
  const parsed = parseTrackId(trackId);
  if (parsed.ok) evictCached(parsed.id);
}

export async function playbackService() {
  registerAudioCacheInvalidator(evictCachedIfParsable);

  TrackPlayer.addEventListener(Event.RemoteDuck, handleRemoteDuck);

  TrackPlayer.addEventListener(Event.RemotePause, () => {
    void TrackPlayer.pause();
  });
  TrackPlayer.addEventListener(
    Event.RemotePlay,
    whenSignedIn(() => {
      void TrackPlayer.play();
    }),
  );
  TrackPlayer.addEventListener(
    Event.RemoteNext,
    whenSignedIn(() => {
      void reportingQueueFailure(currentQueueTrackKey, 'remoteSkipNext', () =>
        withNativeQueue(() => TrackPlayer.skipToNext()),
      );
    }),
  );
  TrackPlayer.addEventListener(
    Event.RemotePrevious,
    whenSignedIn(() => {
      void reportingQueueFailure(currentQueueTrackKey, 'remoteSkipPrevious', playPreviousRemotely);
    }),
  );
  TrackPlayer.addEventListener(
    Event.RemoteSeek,
    whenSignedIn((data: RemoteSeekEvent) => {
      void TrackPlayer.seekTo(data.position);
    }),
  );

  TrackPlayer.addEventListener(Event.PlaybackError, (data) => {
    void handlePlaybackError(data);
  });

  TrackPlayer.addEventListener(Event.PlaybackActiveTrackChanged, (data) => {
    if (typeof data.index !== 'number') return;
    if (!shouldApplyActiveIndex(data.index)) return;
    const key = typeof data.track?.id === 'string' ? data.track.id : undefined;
    useQueueStore.getState().syncCurrentIndex(data.index, key);
    const idx = useQueueStore.getState().currentIndex;
    void prefetchNext(idx);
    void slidePresignWindow(idx);
  });
}
