import TrackPlayer from 'react-native-track-player';

import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import { type TrackKey, trackKey } from '@shared/playback/trackKey';
import type { PlaybackControls, PlaybackTrack } from '@shared/playback/types';

import {
  appendNativeTrack,
  clearNativeQueue,
  insertNativeTrackNext,
  loadNativeQueue,
  loadNativeTrack,
  reorderUpcomingNative,
} from './loadNativeTrack';
import { claimSessionReset } from '../loadToken';
import { withNativeQueue } from './nativeQueueLock';
import { clearPlaybackError, reportLoadFailure } from '../playbackErrorStore';
import { isIndexOutOfBounds, reportingQueueFailure } from './queueFailureReport';
import { redactedPlaybackFailure, warnPlayback } from '../redactPlaybackError';
import { seekPreservingPlayback } from './seekControls';

export interface NativePlaybackActions {
  controls: PlaybackControls;
  syncIsPlaying: (isPlaying: boolean) => void;
  rememberTrack: (track: PlaybackTrack) => void;
}

type SetDisplayedTrack = (track: PlaybackTrack | null) => void;

interface PlaybackMemory {
  lastPlayedTrack: PlaybackTrack | null;
  isPlaying: boolean;
}

export async function ignoringNativeRejection(op: () => Promise<unknown>): Promise<void> {
  try {
    await op();
  } catch (err) {
    console.warn('[playback] native command failed', redactedPlaybackFailure(err));
  }
}

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

function skipToIndexAndPlay(index: number): Promise<void> {
  return withNativeQueue(async () => {
    await TrackPlayer.skip(index);
    await TrackPlayer.play();
  });
}

async function playQueueIndex(index: number): Promise<void> {
  try {
    await skipToIndexAndPlay(index);
  } catch (err) {
    if (!isIndexOutOfBounds(err)) throw err;
    warnPlayback('skip target outside the native queue window; rebuilding', { index }, err);
    const queue = useQueueStore.getState();
    await loadNativeQueue(orderedQueueTracks(queue), index);
  }
}

function removeQueuedIndex(index: number): Promise<void> {
  return withNativeQueue(() => TrackPlayer.remove(index)).catch((err: unknown) => {
    if (!isIndexOutOfBounds(err)) throw err;
  });
}

function createQueueCommands(memory: PlaybackMemory): QueueCommands {
  const currentKey = () => displayedKey(memory);
  return {
    reorderUpcoming: (upcoming) =>
      reportingQueueFailure(currentKey, 'reorderUpcoming', () => reorderUpcomingNative(upcoming)),
    appendToQueue: (track) =>
      reportingQueueFailure(currentKey, 'appendToQueue', () => appendNativeTrack(track)),
    insertNext: (track, position) =>
      reportingQueueFailure(currentKey, 'insertNext', () => insertNativeTrackNext(track, position)),
    skipToQueueIndex: (index) =>
      reportingQueueFailure(currentKey, 'skipToQueueIndex', () => playQueueIndex(index)),
    skipNext: () =>
      reportingQueueFailure(currentKey, 'skipNext', () =>
        withNativeQueue(() => TrackPlayer.skipToNext()),
      ),
    skipPrevious: () =>
      reportingQueueFailure(currentKey, 'skipPrevious', () =>
        withNativeQueue(() => TrackPlayer.skipToPrevious()),
      ),
    removeQueueIndex: (index) =>
      reportingQueueFailure(currentKey, 'removeQueueIndex', () => removeQueuedIndex(index)),
  };
}

type TransportCommands = Pick<PlaybackControls, 'pause' | 'resume' | 'seekTo' | 'setRate' | 'stop'>;

function stopNativePlayback(): Promise<void> {
  claimSessionReset();
  return ignoringNativeRejection(() => withNativeQueue(clearNativeQueue));
}

function movePlaybackTo(positionMs: number, memory: PlaybackMemory): Promise<void> {
  return withNativeQueue(() => seekPreservingPlayback(positionMs / 1000, memory.isPlaying));
}

function createTransportCommands(
  setTrack: SetDisplayedTrack,
  memory: PlaybackMemory,
): TransportCommands {
  return {
    pause: () => {
      void ignoringNativeRejection(() => withNativeQueue(() => TrackPlayer.pause()));
    },
    resume: () => {
      void ignoringNativeRejection(() => withNativeQueue(() => TrackPlayer.play()));
    },
    seekTo: (ms) => {
      void reportingQueueFailure(
        () => displayedKey(memory),
        'seekTo',
        () => movePlaybackTo(ms, memory),
      );
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
