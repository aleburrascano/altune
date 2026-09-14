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
import { withNativeQueue } from './nativeQueueLock';
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

export async function ignoringNativeRejection(op: () => Promise<unknown>): Promise<void> {
  try {
    await op();
  } catch {}
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

  const controls: PlaybackControls = {
    play,
    startQueue,
    reorderUpcoming: (upcoming) => ignoringNativeRejection(() => reorderUpcomingNative(upcoming)),
    appendToQueue: (track) => ignoringNativeRejection(() => appendNativeTrack(track)),
    insertNext: (track, position) =>
      ignoringNativeRejection(() => insertNativeTrackNext(track, position)),
    skipToQueueIndex: (index) =>
      ignoringNativeRejection(() =>
        withNativeQueue(async () => {
          await TrackPlayer.skip(index);
          await TrackPlayer.play();
        }),
      ),
    skipNext: () => ignoringNativeRejection(() => withNativeQueue(() => TrackPlayer.skipToNext())),
    skipPrevious: () =>
      ignoringNativeRejection(() => withNativeQueue(() => TrackPlayer.skipToPrevious())),
    removeQueueIndex: (index) =>
      ignoringNativeRejection(() => withNativeQueue(() => TrackPlayer.remove(index))),
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
