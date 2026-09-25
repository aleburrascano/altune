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
import { claimSessionReset } from './loadToken';
import { withNativeQueue } from './nativeQueueLock';
import { shouldApplyActiveIndex } from './nativeSyncGuard';
import { activeNativeTrackId } from './nativeTrack';
import { forgetAllSwaps, repairActiveToStreaming, wasSwappedToLocal } from './nativeTrackSwap';
import { classifyNativePlaybackError } from './classifyPlaybackError';
import { clearPlaybackError, reportPlaybackError } from './playbackErrorStore';
import { recordPlaybackFailure } from './playbackHealth';
import { reportingQueueFailure, reportQueueFailure } from './queueFailureReport';

// Drops the previous user's playback on sign-out or an account switch: the queue store is a
// module singleton, the native queue (signed URLs + auth headers) lives in a persistent native
// service, and their prefetched audio sits unencrypted on disk, so unmounting the React tree
// clears none of the three. Claiming the reset first makes any in-flight load, append or reorder
// of the old queue bail before it re-adds it, and the disk is cleared ahead of the native reset
// because that reset can reject (#1728) and the files must go either way.
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

// The JS state above is cleared either way, so a rejected reset is the one way sign-out
// still leaves the outgoing user's tracks — their signed URLs and auth headers — loaded on
// a native service that outlives the React tree. `runSignOutCleanups` discards whatever
// this rejects with, so the failure is classified and logged here instead of vanishing
// (#1728), and the reset is attempted once more: a stalled bridge call, or an op that held
// the lock past its deadline, is usually over by the next one. Reported against no track:
// the outgoing user's failure must not surface on the next user's screen.
async function resetNativeQueueForSignOut(): Promise<void> {
  try {
    await resetNativeQueue();
  } catch (err) {
    reportQueueFailure(null, 'signOutReset', err);
    await retryNativeQueueReset();
  }
}

// A reset that fails twice is stuck rather than busy, and no further attempt here can tell
// it apart from a native player that is gone; the log is what is left of it.
async function retryNativeQueueReset(): Promise<void> {
  try {
    await resetNativeQueue();
  } catch (err) {
    reportQueueFailure(null, 'signOutResetRetry', err);
    throw err;
  }
}

// Remote controls (lock screen, Bluetooth, headset) reach the player even while the
// sign-in screen shows. With no user signed in there is nothing of theirs to resume,
// so every command that could start or move audio is a no-op; pause stays allowed.
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

// The presign window is marked refreshed only once the reorder has installed the signed
// URLs, so a rejection leaves it unmarked and the next active-track change retries the
// slide. It is still reported: until a retry lands, the upcoming block holds URLs that
// were never refreshed, and only `retry` rebuilds the native queue from the store.
function slidePresignWindow(index: number): Promise<void> {
  return reportingQueueFailure(currentQueueTrackKey, 'refreshUpcomingPresign', () =>
    refreshUpcomingPresign(index),
  );
}

// One native PlaybackError is usually that track's own problem, and re-presigning or repairing it
// is the right answer. A fleet-wide one is not: a batch of tracks with a broken signed URL, or an
// OS update that kills a codec, makes every track fail, and each failure would otherwise re-hit
// presign/recover the moment it arrives — amplifying load on the endpoints already in trouble,
// with no way to stop it short of a client release (#1745). So each track spends from a budget:
// the first few errors recover at once, and spending the budget earns a cooldown that doubles,
// until a persistently failing track costs the API almost nothing.
export const RECOVERY_ATTEMPTS_PER_TRACK = 2;

/** Cooldown earned once a track's budget is spent; each further attempt doubles it. */
export const RECOVERY_COOLDOWN_BASE_MS = 30_000;

/** Caps a single cooldown, and how long a track that has stopped failing is remembered at all. */
const RECOVERY_MEMORY_MS = 10 * 60_000;

/** So a long queue of failing tracks cannot grow the map without end. */
export const MAX_TRACKED_RECOVERIES = 64;

type RecoveryRun = { attempts: number; lastAttemptAt: number };

const recoveryRuns = new Map<TrackKey, RecoveryRun>();

// A device whose clock jumps backwards (NTP, a manual change) must not freeze recovery until the
// clock catches up, so a negative elapsed settles the run rather than extending its cooldown.
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

// Asking spends the attempt. A device already tracking `MAX_TRACKED_RECOVERIES` failing tracks is
// in exactly the fleet-wide failure this budget exists for, so one more track is refused rather
// than evicting a track that is mid-cooldown.
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
  // Tallied before the key is known to be one: a failure with no track to show it on is the one
  // the user can least report, so telemetry is all that carries it.
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

// Mirror an audio interruption (e.g. a phone call) onto the player. Pause when it
// begins, resume when it ends. iOS 18's native auto-resume after a call is
// unreliable even with autoHandleInterruptions, so this play() acts as the resume;
// both calls are idempotent, so they reinforce rather than fight one another. A
// permanent focus loss means another app took over — stay paused.
function handleRemoteDuck(data: RemoteDuckEvent): void {
  if (data.permanent) return;
  if (data.paused) {
    void TrackPlayer.pause();
    return;
  }
  if (!hasSignedInUser()) return;
  void TrackPlayer.play();
}

// A remote "previous" past the restart threshold restarts the current track instead of
// stepping back, the rule FullPlayer's own previous button applies.
async function playPreviousRemotely(): Promise<void> {
  const { position } = await TrackPlayer.getProgress();
  if (shouldRestartOnPrevious(position * 1000)) {
    await TrackPlayer.seekTo(0);
    return;
  }
  await withNativeQueue(() => TrackPlayer.skipToPrevious());
}

// The invalidator registry carries ids as bare strings, so the brand is re-established here.
// Nothing is lost when it fails: a cache file is only ever named after a parsed id, so an id of
// any other shape has no file to evict and no swap to forget.
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
  // A skip from the lock screen, a car or a headset runs the same native queue mutation
  // as the in-app buttons, so it is classified, logged and surfaced the same way (#1742):
  // swallowing it leaves a dead button with nothing in the logs to explain it.
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
