import TrackPlayer, {
  Event,
  type PlaybackErrorEvent,
  type RemoteDuckEvent,
  type RemoteSeekEvent,
} from 'react-native-track-player';

import { shouldRestartOnPrevious } from '@features/playback/domain/restartOnPrevious';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import { type TrackKey, trackKey, type PlaybackTrack } from '@shared/playback/playbackTrack';

import { registerAudioCacheInvalidator } from '@shared/acquisition/audioCacheInvalidation';
import { parseTrackId } from '@shared/api-client/ids';
import { playbackPort } from '@features/playback/ports/activePlaybackPort';
import { hasSignedInUser, onSignOut } from '@shared/session/signOutCleanup';
import {
  discardPrefetchedAudio,
  evictCached,
  prefetchNext,
} from '@features/playback/native/audioPrefetch';
import { refreshUpcomingPresign } from '@features/playback/native/loadNativeTrack';
import {
  claimRecoveryAttempt,
  resetRecoveryBudget,
} from '@features/playback/native/recoveryBudget';
import { claimSessionReset } from '@features/playback/domain/loadToken';
import { withNativeQueue } from '@features/playback/native/nativeQueueLock';
import { shouldApplyActiveIndex } from '@features/playback/native/nativeSyncGuard';
import { activeNativeTrackId } from '@features/playback/native/nativeTrack';
import {
  forgetAllSwaps,
  repairActiveToStreaming,
  wasSwappedToLocal,
} from '@features/playback/native/nativeTrackSwap';
import {
  classifyNativePlaybackError,
  redactedPlaybackFailure,
  redactPlaybackErrorMessage,
} from '@features/playback/domain/playbackError';
import {
  clearPlaybackError,
  reportPlaybackError,
} from '@features/playback/hooks/usePlaybackErrorStore';
import {
  recordAudioRecoveryFailure,
  recordPlaybackFailure,
} from '@features/playback/domain/playbackHealth';
import { progressSecondsToMs } from '@features/playback/domain/time';
import {
  reportingQueueFailure,
  reportQueueFailure,
} from '@features/playback/native/queueFailureReport';

export async function resetPlaybackForSignOut(): Promise<void> {
  claimSessionReset();
  useQueueStore.getState().clearQueue();
  clearPlaybackError();
  resetRecoveryBudget();
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

async function failedTrackAndKey(): Promise<[PlaybackTrack | null, TrackKey | null]> {
  const key = (await activeNativeTrackId()) ?? null;
  const failed = key !== null ? queueTrackByKey(key) : useQueueStore.getState().currentTrack();
  return [failed, key ?? (failed ? trackKey(failed) : null)];
}

function recordNativeFailure(event: PlaybackErrorEvent, failedKey: TrackKey | null): void {
  const { code, message } = event;
  const kind = classifyNativePlaybackError(code ?? '', message ?? '');
  recordPlaybackFailure(kind);
  if (kind === 'unknown') warnUnmappedPlaybackError(event, failedKey);
  if (failedKey !== null) reportPlaybackError(failedKey, kind, message || 'Playback failed');
}

async function recoverFailedTrack(failed: PlaybackTrack | null): Promise<void> {
  if (!failed || failed.source.kind !== 'library') return;
  if (!claimRecoveryAttempt(trackKey(failed), Date.now())) return;
  if (wasSwappedToLocal(failed.source.trackId)) {
    await repairActiveToStreaming(failed);
    return;
  }
  await playbackPort().recoverAudio(failed.source.trackId).catch(warnAudioRecoveryFailed);
}

async function handlePlaybackError(event: PlaybackErrorEvent): Promise<void> {
  const [failed, failedKey] = await failedTrackAndKey();
  recordNativeFailure(event, failedKey);
  await recoverFailedTrack(failed);
}

function warnUnmappedPlaybackError(
  { code, message }: PlaybackErrorEvent,
  failedKey: TrackKey | null,
): void {
  console.warn('[playback] unmapped native error', {
    code: redactPlaybackErrorMessage(code ?? ''),
    trackKey: failedKey && redactPlaybackErrorMessage(failedKey),
    error: redactPlaybackErrorMessage(message ?? ''),
  });
}

function warnAudioRecoveryFailed(err: unknown): void {
  console.warn('[playback] audio recovery failed', { error: redactedPlaybackFailure(err) });
  recordAudioRecoveryFailure();
}

function reportingRemoteCommand(op: string, run: () => Promise<unknown>): void {
  void reportingQueueFailure(currentQueueTrackKey, op, run);
}

function handleRemoteDuck(data: RemoteDuckEvent): void {
  if (data.permanent) return;
  if (data.paused) {
    reportingRemoteCommand('remoteDuckPause', () => TrackPlayer.pause());
    return;
  }
  if (!hasSignedInUser()) return;
  reportingRemoteCommand('remoteDuckPlay', () => TrackPlayer.play());
}

async function playPreviousRemotely(): Promise<void> {
  const { position } = await TrackPlayer.getProgress();
  if (shouldRestartOnPrevious(progressSecondsToMs(position))) {
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
    reportingRemoteCommand('remotePause', () => TrackPlayer.pause());
  });
  TrackPlayer.addEventListener(
    Event.RemotePlay,
    whenSignedIn(() => {
      reportingRemoteCommand('remotePlay', () => TrackPlayer.play());
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
      reportingRemoteCommand('remoteSeek', () => TrackPlayer.seekTo(data.position));
    }),
  );

  TrackPlayer.addEventListener(Event.PlaybackError, (data) => {
    reportingRemoteCommand('handlePlaybackError', () => handlePlaybackError(data));
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

export function registerPlaybackService(): void {
  TrackPlayer.registerPlaybackService(() => playbackService);
  onSignOut(resetPlaybackForSignOut);
}
