import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import TrackPlayer, {
  RepeatMode,
  State,
  usePlaybackState,
  useProgress,
} from 'react-native-track-player';

import { PlaybackContext } from '@shared/playback/PlaybackContext';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import type {
  PlaybackContextValue,
  PlaybackState,
  PlaybackTrack,
  RepeatMode as QueueRepeatMode,
} from '@shared/playback/types';

import { derivePlaybackState } from '../derivePlaybackState';
import { ensurePlayerSetup } from '../initPlayer';
import { withNativeQueue } from '../nativeQueueLock';
import { seekPreservingPlayback } from '../seekControls';
import {
  appendNativeTrack,
  insertNativeTrackNext,
  loadNativeQueue,
  loadNativeTrack,
  reorderUpcomingNative,
} from '../loadNativeTrack';
import {
  clearPlaybackError,
  reportPlaybackError,
  usePlaybackErrorFor,
} from '../playbackErrorStore';
import { useIsForeground } from './useIsForeground';
import { usePlaybackSignals } from './usePlaybackSignals';
import { useQueueResume } from './useQueueResume';

const NATIVE_REPEAT: Record<QueueRepeatMode, RepeatMode> = {
  off: RepeatMode.Off,
  all: RepeatMode.Queue,
  one: RepeatMode.Track,
};

function loadFailureMessage(err: unknown): string {
  return err instanceof Error ? err.message : 'Failed to load audio';
}

async function ignoringNativeRejection(op: () => Promise<unknown>): Promise<void> {
  try {
    await op();
  } catch {}
}

export function TrackPlayerPlaybackProvider({ children }: { children: ReactNode }) {
  const [track, setTrack] = useState<PlaybackTrack | null>(null);
  const errorMessage = usePlaybackErrorFor(track ? trackKey(track) : null);
  const lastPlayedTrack = useRef<PlaybackTrack | null>(null);

  const playbackState = usePlaybackState();
  const progress = useProgress(500);
  const isForeground = useIsForeground();

  useEffect(() => {
    void ensurePlayerSetup();
  }, []);

  const frozenPositionMs = useRef(0);
  const livePositionMs = progress.position * 1000;
  const resumePositionMs = useQueueStore((s) => s.resumePositionMs);
  const displayPositionMs = livePositionMs > 0 ? livePositionMs : resumePositionMs;
  useEffect(() => {
    if (livePositionMs > 0 && resumePositionMs > 0) {
      useQueueStore.getState().setResumePosition(0);
    }
  }, [livePositionMs, resumePositionMs]);
  if (isForeground) frozenPositionMs.current = displayPositionMs;
  const positionMs = isForeground ? displayPositionMs : frozenPositionMs.current;
  const rawDurationMs = progress.duration * 1000;

  const trackDurationMs =
    track?.durationSeconds != null && Number.isFinite(track.durationSeconds)
      ? track.durationSeconds * 1000
      : 0;

  const durationMs = rawDurationMs || trackDurationMs;

  const tpState = playbackState.state;
  const isPlaying = tpState === State.Playing;
  const isBuffering = tpState === State.Buffering || tpState === State.Loading;
  const isEnded = tpState === State.Ended;

  const isPlayingRef = useRef(isPlaying);
  isPlayingRef.current = isPlaying;

  const state: PlaybackState = useMemo(
    () =>
      derivePlaybackState({
        track,
        errorMessage,
        isBuffering,
        isEnded,
        isPlaying,
        positionMs,
        durationMs,
      }),
    [track, errorMessage, isEnded, isPlaying, isBuffering, positionMs, durationMs],
  );

  usePlaybackSignals({
    track,
    positionMs: livePositionMs,
    durationMs,
  });

  const play = useCallback(async (newTrack: PlaybackTrack) => {
    clearPlaybackError();
    setTrack(newTrack);
    lastPlayedTrack.current = newTrack;
    useQueueStore.getState().clearQueue();

    try {
      await loadNativeTrack(newTrack);
    } catch (err) {
      reportPlaybackError(trackKey(newTrack), loadFailureMessage(err));
    }
  }, []);

  const startQueue = useCallback<PlaybackContextValue['startQueue']>(
    async (orderedTracks, startIndex, options) => {
      clearPlaybackError();
      try {
        await loadNativeQueue(orderedTracks, startIndex, options);
      } catch (err) {
        const failed = orderedTracks[startIndex];
        if (failed) reportPlaybackError(trackKey(failed), loadFailureMessage(err));
      }
    },
    [],
  );

  const reorderUpcoming = useCallback<PlaybackContextValue['reorderUpcoming']>(
    (upcoming) => ignoringNativeRejection(() => reorderUpcomingNative(upcoming)),
    [],
  );

  const appendToQueue = useCallback<PlaybackContextValue['appendToQueue']>(
    (track) => ignoringNativeRejection(() => appendNativeTrack(track)),
    [],
  );

  const insertNext = useCallback<PlaybackContextValue['insertNext']>(
    (track, position) => ignoringNativeRejection(() => insertNativeTrackNext(track, position)),
    [],
  );

  const skipToQueueIndex = useCallback(
    (index: number) =>
      ignoringNativeRejection(() =>
        withNativeQueue(async () => {
          await TrackPlayer.skip(index);
          await TrackPlayer.play();
        }),
      ),
    [],
  );

  const skipNext = useCallback(
    () => ignoringNativeRejection(() => withNativeQueue(() => TrackPlayer.skipToNext())),
    [],
  );

  const skipPrevious = useCallback(
    () => ignoringNativeRejection(() => withNativeQueue(() => TrackPlayer.skipToPrevious())),
    [],
  );

  const removeQueueIndex = useCallback(
    (index: number) => ignoringNativeRejection(() => withNativeQueue(() => TrackPlayer.remove(index))),
    [],
  );

  const pause = useCallback(() => {
    void TrackPlayer.pause();
  }, []);
  const resume = useCallback(() => {
    void TrackPlayer.play();
  }, []);
  const seekTo = useCallback((ms: number) => {
    void seekPreservingPlayback(ms / 1000, isPlayingRef.current);
  }, []);

  const setRate = useCallback((rate: number) => {
    void ignoringNativeRejection(() => TrackPlayer.setRate(rate));
  }, []);

  const stop = useCallback(() => {
    void TrackPlayer.reset();
    setTrack(null);
    clearPlaybackError();
  }, []);

  const retry = useCallback(() => {
    clearPlaybackError();
    const s = useQueueStore.getState();
    if (s.currentTrack()) {
      void startQueue(orderedQueueTracks(s), s.currentIndex);
      return;
    }
    const trackToRetry = lastPlayedTrack.current;
    if (trackToRetry) void play(trackToRetry);
  }, [play, startQueue]);

  const repeatMode = useQueueStore((s) => s.repeatMode);
  useEffect(() => {
    void ignoringNativeRejection(async () => {
      await ensurePlayerSetup();
      await TrackPlayer.setRepeatMode(NATIVE_REPEAT[repeatMode]);
    });
  }, [repeatMode]);

  const currentQueueTrack = useQueueStore((s) => s.currentTrack());

  useEffect(() => {
    if (!currentQueueTrack) return;
    setTrack(currentQueueTrack);
    lastPlayedTrack.current = currentQueueTrack;
  }, [currentQueueTrack]);

  useQueueResume();

  const value = useMemo<PlaybackContextValue>(
    () => ({
      ...state,
      play,
      startQueue,
      reorderUpcoming,
      appendToQueue,
      insertNext,
      skipToQueueIndex,
      skipNext,
      skipPrevious,
      removeQueueIndex,
      pause,
      resume,
      seekTo,
      setRate,
      stop,
      retry,
    }),
    [
      state,
      play,
      startQueue,
      reorderUpcoming,
      appendToQueue,
      insertNext,
      skipToQueueIndex,
      skipNext,
      skipPrevious,
      removeQueueIndex,
      pause,
      resume,
      seekTo,
      setRate,
      stop,
      retry,
    ],
  );

  return <PlaybackContext.Provider value={value}>{children}</PlaybackContext.Provider>;
}
