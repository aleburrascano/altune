import TrackPlayer from 'react-native-track-player';

import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import { type TrackKey, trackKey } from '@shared/playback/trackKey';
import type { PlaybackControls, PlaybackErrorKind, PlaybackTrack } from '@shared/playback/types';

import {
  appendNativeTrack,
  insertNativeTrackNext,
  loadNativeQueue,
  loadNativeTrack,
  reorderUpcomingNative,
} from './loadNativeTrack';
import { claimSessionReset } from './loadToken';
import { NativeQueueTimeoutError, withNativeQueue } from './nativeQueueLock';
import { clearPlaybackError, reportLoadFailure, reportPlaybackError } from './playbackErrorStore';
import { recordPlaybackFailure } from './playbackHealth';
import { seekPreservingPlayback } from './seekControls';

export interface NativePlaybackActions {
  /** The command half of the playback context value. */
  controls: PlaybackControls;
  /** Records whether native playback is playing, read later by `seekTo`. */
  syncIsPlaying: (isPlaying: boolean) => void;
  /** Records the displayed track so `retry` can replay it outside a queue. */
  rememberTrack: (track: PlaybackTrack) => void;
}

type SetDisplayedTrack = (track: PlaybackTrack | null) => void;

/** The one mutable cell every command family shares; one per created action set. */
interface PlaybackMemory {
  lastPlayedTrack: PlaybackTrack | null;
  isPlaying: boolean;
}

/**
 * For native calls whose failure the caller cannot act on (pause, resume, rate, the
 * `stop` reset): the rejection must not crash a UI handler, but it is still logged,
 * never discarded.
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

const QUEUE_FAILURE_REPORT: Record<
  NativeQueueFailureKind,
  { errorKind: PlaybackErrorKind; message: string }
> = {
  permanent: { errorKind: 'queue_out_of_sync', message: QUEUE_OUT_OF_SYNC_MESSAGE },
  transient: { errorKind: 'queue_update_failed', message: QUEUE_UPDATE_FAILED_MESSAGE },
};

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

/** The commands that load audio: each reports its own failure against the failed track. */
type LoadCommands = Pick<PlaybackControls, 'play' | 'startQueue' | 'retry'>;

const startQueue: PlaybackControls['startQueue'] = async (orderedTracks, startIndex, options) => {
  clearPlaybackError();
  try {
    await loadNativeQueue(orderedTracks, startIndex, options);
  } catch (err) {
    const failed = orderedTracks[startIndex];
    if (failed) reportLoadFailure(failed, err);
  }
};

function createLoadCommands(setTrack: SetDisplayedTrack, memory: PlaybackMemory): LoadCommands {
  const play: PlaybackControls['play'] = async (newTrack) => {
    clearPlaybackError();
    setTrack(newTrack);
    memory.lastPlayedTrack = newTrack;
    useQueueStore.getState().clearQueue();

    try {
      await loadNativeTrack(newTrack);
    } catch (err) {
      reportLoadFailure(newTrack, err);
    }
  };

  const retry: PlaybackControls['retry'] = () => {
    clearPlaybackError();
    const queue = useQueueStore.getState();
    if (queue.currentTrack()) {
      void startQueue(orderedQueueTracks(queue), queue.currentIndex);
      return;
    }
    const trackToRetry = memory.lastPlayedTrack;
    if (trackToRetry) void play(trackToRetry);
  };

  return { play, startQueue, retry };
}

/** The commands that move or reshape the native queue the store already mutated. */
type QueueCommands = Pick<
  PlaybackControls,
  | 'reorderUpcoming'
  | 'appendToQueue'
  | 'insertNext'
  | 'skipToQueueIndex'
  | 'skipNext'
  | 'skipPrevious'
  | 'removeQueueIndex'
>;

function displayedKey(memory: PlaybackMemory): TrackKey | null {
  const displayed = useQueueStore.getState().currentTrack() ?? memory.lastPlayedTrack;
  return displayed ? trackKey(displayed) : null;
}

/**
 * The one way a failed native queue mutation is surfaced: classified, logged, and shown
 * on `key` — the track whose error state offers `retry`, which rebuilds the native queue
 * from the store. No automatic retry here. A null `key` is logged and tallied only, for a
 * failure that can no longer be attributed to the track on screen.
 */
export function reportQueueFailure(key: TrackKey | null, op: string, err: unknown): void {
  const kind = classifyNativeQueueFailure(err);
  const { errorKind, message } = QUEUE_FAILURE_REPORT[kind];
  console.warn('[playback] native queue mutation failed', {
    op,
    kind,
    code: nativeErrorCode(err),
    error: err,
  });
  recordPlaybackFailure(errorKind);
  if (key === null) return;
  reportPlaybackError(key, errorKind, message);
}

/**
 * The caller already mutated queueStore optimistically, so a rejected native
 * mutation leaves the two drifted. Never reject into the UI handler.
 */
async function reportingQueueFailure(
  memory: PlaybackMemory,
  op: string,
  run: () => Promise<unknown>,
): Promise<void> {
  const keyAtCall = displayedKey(memory);
  try {
    await run();
  } catch (err) {
    // A queued op can settle after a newer load replaced the queue; its failure
    // says nothing about the track now displayed, so it is only logged.
    const isStillDisplayed = keyAtCall !== null && keyAtCall === displayedKey(memory);
    reportQueueFailure(isStillDisplayed ? keyAtCall : null, op, err);
  }
}

function skipToIndexAndPlay(index: number): Promise<void> {
  return withNativeQueue(async () => {
    await TrackPlayer.skip(index);
    await TrackPlayer.play();
  });
}

/**
 * Native holds a window of the store queue, so a target the user picked further down it
 * is not drift: rebuild the native queue around that position and play there — the same
 * recovery `retry` performs, without the tap. A rebuild that itself fails still rejects,
 * so a genuinely broken queue is reported rather than retried forever.
 */
async function playQueueIndex(index: number): Promise<void> {
  try {
    await skipToIndexAndPlay(index);
  } catch (err) {
    console.warn('[playback] skip target outside the native queue window; rebuilding', {
      index,
      error: err,
    });
    const queue = useQueueStore.getState();
    await loadNativeQueue(orderedQueueTracks(queue), index);
  }
}

/**
 * A position past the native window holds nothing to remove: the store has already
 * dropped the track and the next window slide rebuilds the tail without it.
 */
function removeQueuedIndex(index: number): Promise<void> {
  return withNativeQueue(() => TrackPlayer.remove(index)).catch((err: unknown) => {
    if (nativeErrorCode(err) !== 'index_out_of_bounds') throw err;
  });
}

function createQueueCommands(memory: PlaybackMemory): QueueCommands {
  return {
    reorderUpcoming: (upcoming) =>
      reportingQueueFailure(memory, 'reorderUpcoming', () => reorderUpcomingNative(upcoming)),
    appendToQueue: (track) =>
      reportingQueueFailure(memory, 'appendToQueue', () => appendNativeTrack(track)),
    insertNext: (track, position) =>
      reportingQueueFailure(memory, 'insertNext', () => insertNativeTrackNext(track, position)),
    skipToQueueIndex: (index) =>
      reportingQueueFailure(memory, 'skipToQueueIndex', () => playQueueIndex(index)),
    skipNext: () =>
      reportingQueueFailure(memory, 'skipNext', () =>
        withNativeQueue(() => TrackPlayer.skipToNext()),
      ),
    skipPrevious: () =>
      reportingQueueFailure(memory, 'skipPrevious', () =>
        withNativeQueue(() => TrackPlayer.skipToPrevious()),
      ),
    removeQueueIndex: (index) =>
      reportingQueueFailure(memory, 'removeQueueIndex', () => removeQueuedIndex(index)),
  };
}

/** The commands that act on what is already loaded and never await the native call. */
type TransportCommands = Pick<PlaybackControls, 'pause' | 'resume' | 'seekTo' | 'setRate' | 'stop'>;

/**
 * An unlocked reset can cut into an in-flight load's own add/skip/play, and a queue
 * edit that resolved its URLs before the stop would refill the queue after it.
 * Claiming the reset first makes both bail, the way sign-out's reset does.
 */
function stopNativePlayback(): Promise<void> {
  claimSessionReset();
  return ignoringNativeRejection(() => withNativeQueue(() => TrackPlayer.reset()));
}

/**
 * Unserialized, two rapid seeks can reach native in either order, so the position the
 * user asked for last is not the one that sticks; the lock also keeps the seek's own
 * seek/play pair out of a concurrent add, skip or reset. Playback state is read when the
 * op runs, so a pause while the seek waited for the lock is honoured.
 */
function movePlaybackTo(positionMs: number, memory: PlaybackMemory): Promise<void> {
  return withNativeQueue(() => seekPreservingPlayback(positionMs / 1000, memory.isPlaying));
}

function createTransportCommands(
  setTrack: SetDisplayedTrack,
  memory: PlaybackMemory,
): TransportCommands {
  return {
    pause: () => {
      void ignoringNativeRejection(() => TrackPlayer.pause());
    },
    resume: () => {
      void ignoringNativeRejection(() => TrackPlayer.play());
    },
    seekTo: (ms) => {
      void reportingQueueFailure(memory, 'seekTo', () => movePlaybackTo(ms, memory));
    },
    setRate: (rate) => {
      void ignoringNativeRejection(() => TrackPlayer.setRate(rate));
    },
    stop: () => {
      void stopNativePlayback();
      setTrack(null);
      clearPlaybackError();
    },
  };
}

/**
 * Builds the react-native-track-player command set for the playback context.
 * `setTrack` replaces the caller's displayed track; `initialIsPlaying` seeds
 * what `seekTo` assumes until the first `syncIsPlaying`. Each call creates fresh
 * memory, so callers create it once per provider.
 */
export function createNativePlaybackActions(
  setTrack: SetDisplayedTrack,
  initialIsPlaying = false,
): NativePlaybackActions {
  const memory: PlaybackMemory = { lastPlayedTrack: null, isPlaying: initialIsPlaying };

  return {
    controls: {
      ...createLoadCommands(setTrack, memory),
      ...createQueueCommands(memory),
      ...createTransportCommands(setTrack, memory),
    },
    syncIsPlaying: (isPlaying) => {
      memory.isPlaying = isPlaying;
    },
    rememberTrack: (track) => {
      memory.lastPlayedTrack = track;
    },
  };
}
