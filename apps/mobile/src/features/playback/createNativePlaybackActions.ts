import TrackPlayer from 'react-native-track-player';

import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackControls, PlaybackTrack } from '@shared/playback/types';

import {
  appendNativeTrack,
  insertNativeTrackNext,
  loadNativeQueue,
  loadNativeTrack,
  reorderUpcomingNative,
} from './loadNativeTrack';
import { NativeQueueTimeoutError, withNativeQueue } from './nativeQueueLock';
import { clearPlaybackError, reportPlaybackError } from './playbackErrorStore';
import { seekPreservingPlayback } from './seekControls';

export interface NativePlaybackActions {
  /** The command half of the playback context value. */
  controls: PlaybackControls;
  /** Records whether native playback is playing, read later by `seekTo`. */
  syncIsPlaying: (isPlaying: boolean) => void;
  /** Records the displayed track so `retry` can replay it outside a queue. */
  rememberTrack: (track: PlaybackTrack) => void;
}

function loadFailureMessage(err: unknown): string {
  return err instanceof Error ? err.message : 'Failed to load audio';
}

/**
 * For native calls whose failure leaves nothing drifted (rate, repeat mode): the
 * rejection must not crash a UI handler, but it is still logged, never discarded.
 */
export async function ignoringNativeRejection(op: () => Promise<unknown>): Promise<void> {
  try {
    await op();
  } catch (err) {
    console.warn('[playback] native command failed', err);
  }
}

export type NativeQueueFailureKind = 'transient' | 'permanent';

// react-native-track-player rejection codes (iOS + Android) meaning the native
// queue no longer matches what the caller assumed: a stale index, a missing active
// item, a torn-down player, or a malformed item. Repeating the same call cannot
// succeed; only rebuilding the native queue from the store (retry) recovers.
const PERMANENT_NATIVE_CODES: ReadonlySet<string> = new Set([
  'index_out_of_bounds',
  'no_current_item',
  'player_not_initialized',
  'invalid_track_object',
]);

export const QUEUE_UPDATE_FAILED_MESSAGE = "Couldn't update the queue. Tap retry to resync.";
export const QUEUE_OUT_OF_SYNC_MESSAGE = 'The queue fell out of sync. Tap retry to reload it.';

function nativeErrorCode(err: unknown): string | null {
  if (typeof err !== 'object' || err === null || !('code' in err)) return null;
  return typeof err.code === 'string' ? err.code : null;
}

/**
 * Timeouts, unknown bridge errors and non-error rejections are treated as transient;
 * only a native code that proves the queue diverged is permanent.
 */
export function classifyNativeQueueFailure(err: unknown): NativeQueueFailureKind {
  if (err instanceof NativeQueueTimeoutError) return 'transient';
  const code = nativeErrorCode(err);
  return code !== null && PERMANENT_NATIVE_CODES.has(code) ? 'permanent' : 'transient';
}

/**
 * Builds the react-native-track-player command set for the playback context.
 * `setTrack` replaces the caller's displayed track; `initialIsPlaying` seeds
 * what `seekTo` assumes until the first `syncIsPlaying`. Each call creates fresh
 * callbacks and fresh memory, so callers create it once per provider.
 */
export function createNativePlaybackActions(
  setTrack: (track: PlaybackTrack | null) => void,
  initialIsPlaying = false,
): NativePlaybackActions {
  let lastPlayedTrack: PlaybackTrack | null = null;
  let isPlaying = initialIsPlaying;

  const play = async (newTrack: PlaybackTrack) => {
    clearPlaybackError();
    setTrack(newTrack);
    lastPlayedTrack = newTrack;
    useQueueStore.getState().clearQueue();

    try {
      await loadNativeTrack(newTrack);
    } catch (err) {
      reportPlaybackError(trackKey(newTrack), loadFailureMessage(err));
    }
  };

  const startQueue: PlaybackControls['startQueue'] = async (orderedTracks, startIndex, options) => {
    clearPlaybackError();
    try {
      await loadNativeQueue(orderedTracks, startIndex, options);
    } catch (err) {
      const failed = orderedTracks[startIndex];
      if (failed) reportPlaybackError(trackKey(failed), loadFailureMessage(err));
    }
  };

  // The caller already mutated queueStore optimistically, so a rejected native
  // mutation leaves the two drifted. Never reject into the UI handler, but surface
  // the failure on the displayed track: its error state offers `retry`, which
  // rebuilds the native queue from the store. No automatic retry here.
  const displayedKey = (): string | null => {
    const displayed = useQueueStore.getState().currentTrack() ?? lastPlayedTrack;
    return displayed ? trackKey(displayed) : null;
  };

  const reportingQueueFailure = async (op: string, run: () => Promise<unknown>) => {
    const keyAtCall = displayedKey();
    try {
      await run();
    } catch (err) {
      const kind = classifyNativeQueueFailure(err);
      console.warn('[playback] native queue mutation failed', {
        op,
        kind,
        code: nativeErrorCode(err),
        error: err,
      });
      // A queued op can settle after a newer load replaced the queue; its failure
      // says nothing about the track now displayed, so it is only logged.
      if (keyAtCall === null || keyAtCall !== displayedKey()) return;
      reportPlaybackError(
        keyAtCall,
        kind === 'permanent' ? QUEUE_OUT_OF_SYNC_MESSAGE : QUEUE_UPDATE_FAILED_MESSAGE,
      );
    }
  };

  const controls: PlaybackControls = {
    play,
    startQueue,
    reorderUpcoming: (upcoming) =>
      reportingQueueFailure('reorderUpcoming', () => reorderUpcomingNative(upcoming)),
    appendToQueue: (track) =>
      reportingQueueFailure('appendToQueue', () => appendNativeTrack(track)),
    insertNext: (track, position) =>
      reportingQueueFailure('insertNext', () => insertNativeTrackNext(track, position)),
    skipToQueueIndex: (index) =>
      reportingQueueFailure('skipToQueueIndex', () =>
        withNativeQueue(async () => {
          await TrackPlayer.skip(index);
          await TrackPlayer.play();
        }),
      ),
    skipNext: () =>
      reportingQueueFailure('skipNext', () => withNativeQueue(() => TrackPlayer.skipToNext())),
    skipPrevious: () =>
      reportingQueueFailure('skipPrevious', () =>
        withNativeQueue(() => TrackPlayer.skipToPrevious()),
      ),
    removeQueueIndex: (index) =>
      reportingQueueFailure('removeQueueIndex', () =>
        withNativeQueue(() => TrackPlayer.remove(index)),
      ),
    pause: () => {
      void TrackPlayer.pause();
    },
    resume: () => {
      void TrackPlayer.play();
    },
    seekTo: (ms) => {
      void seekPreservingPlayback(ms / 1000, isPlaying);
    },
    setRate: (rate) => {
      void ignoringNativeRejection(() => TrackPlayer.setRate(rate));
    },
    stop: () => {
      void TrackPlayer.reset();
      setTrack(null);
      clearPlaybackError();
    },
    retry: () => {
      clearPlaybackError();
      const s = useQueueStore.getState();
      if (s.currentTrack()) {
        void startQueue(orderedQueueTracks(s), s.currentIndex);
        return;
      }
      const trackToRetry = lastPlayedTrack;
      if (trackToRetry) void play(trackToRetry);
    },
  };

  return {
    controls,
    syncIsPlaying: (playing) => {
      isPlaying = playing;
    },
    rememberTrack: (track) => {
      lastPlayedTrack = track;
    },
  };
}
