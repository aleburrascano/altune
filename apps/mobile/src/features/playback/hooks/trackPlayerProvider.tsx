import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import TrackPlayer, {
  RepeatMode,
  State,
  usePlaybackState,
  useProgress,
} from 'react-native-track-player';

import { PlaybackContext } from '@shared/playback/PlaybackContext';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import type {
  PlaybackContextValue,
  PlaybackState,
  PlaybackTrack,
  RepeatMode as QueueRepeatMode,
} from '@shared/playback/types';

import { derivePlaybackState } from '../derivePlaybackState';
import { ensurePlayerSetup } from '../initPlayer';
import { seekPreservingPlayback } from '../seekControls';
import {
  appendNativeTrack,
  insertNativeTrackNext,
  loadNativeQueue,
  loadNativeTrack,
  reorderUpcomingNative,
} from '../loadNativeTrack';
import { useIsForeground } from './useIsForeground';
import { usePlaybackSignals } from './usePlaybackSignals';
import { useQueueResume } from './useQueueResume';

const NATIVE_REPEAT: Record<QueueRepeatMode, RepeatMode> = {
  off: RepeatMode.Off,
  all: RepeatMode.Queue,
  one: RepeatMode.Track,
};

// AIDEV-NOTE: The real, track-player-backed playback provider. It is imported
// ONLY outside Expo Go (via the PlaybackProvider selector), because the
// top-level `react-native-track-player` import touches a native module that
// Expo Go does not bundle. In Expo Go, expoGoPlaybackProvider is used instead.

export function TrackPlayerPlaybackProvider({ children }: { children: ReactNode }) {
  const [track, setTrack] = useState<PlaybackTrack | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const lastPlayedTrack = useRef<PlaybackTrack | null>(null);

  const playbackState = usePlaybackState();
  const progress = useProgress(500);
  const isForeground = useIsForeground();

  useEffect(() => {
    void ensurePlayerSetup();
  }, []);

  // While backgrounded, freeze positionMs at its last foreground value so the
  // context value stops changing twice a second. useProgress still fires its
  // own setState, but with a stable positionMs the memoized state object below
  // keeps its identity — so no usePlayback() consumer re-renders and no
  // JS-thread scrubber/mini-player animation runs. This is what keeps a locked,
  // music-playing app from tripping iOS's background CPU watchdog. The native
  // player (and its lock-screen position) is driven natively, unaffected.
  const frozenPositionMs = useRef(0);
  const livePositionMs = progress.position * 1000;
  // Before the native player loads (progress is 0), show the saved resume offset
  // so the scrubber lands at the right spot on relaunch instead of snapping from
  // 0 a beat later. Once native progress goes live (> 0), it always wins — so the
  // resume seed never fights real playback. Display-only: usePlaybackSignals below
  // still reads the raw livePositionMs so the listen threshold isn't spoofed.
  const resumePositionMs = useQueueStore((s) => s.resumePositionMs);
  const displayPositionMs = livePositionMs > 0 ? livePositionMs : resumePositionMs;
  if (isForeground) frozenPositionMs.current = displayPositionMs;
  const positionMs = isForeground ? displayPositionMs : frozenPositionMs.current;
  const rawDurationMs = progress.duration * 1000;

  // The track carries its own duration (set at queue-build time by
  // toPlaybackTrack), so playback never reaches into the library feature's
  // React Query cache. The native player's reported duration wins once known;
  // the track value fills the gap before the stream's metadata loads.
  const trackDurationMs =
    track?.durationSeconds != null && Number.isFinite(track.durationSeconds)
      ? track.durationSeconds * 1000
      : 0;

  const durationMs = rawDurationMs || trackDurationMs;

  const tpState = playbackState.state;
  const isPlaying = tpState === State.Playing;
  const isBuffering = tpState === State.Buffering || tpState === State.Loading;
  const isEnded = tpState === State.Ended;

  // Latest committed playing state, read by seekTo to decide whether to
  // re-assert play() after a seek (see seekPreservingPlayback).
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

  // Behavioral play/skip/completed are derived from live playback state (listen
  // threshold + dwell), not fired on play-start — see usePlaybackSignals. The
  // live position is used so the 30s/50% threshold is measured against real
  // listening, independent of the frozen-for-render positionMs above.
  usePlaybackSignals({
    track,
    positionMs: livePositionMs,
    durationMs,
  });

  const play = useCallback(async (newTrack: PlaybackTrack) => {
    setErrorMessage(null);
    setTrack(newTrack);
    lastPlayedTrack.current = newTrack;
    // loadNativeTrack reset()s the native queue, so the store's queue is gone
    // too — clear it in the same tick. Leaving it would make the store describe
    // a queue the player no longer holds: the add()-induced ActiveTrackChanged(0)
    // would sync currentIndex to 0 and repoint the UI at the old queue's first
    // track while this one plays. With the queue cleared, syncCurrentIndex(0)
    // no-ops (empty playOrder) and retry() correctly falls back to this track.
    useQueueStore.getState().clearQueue();

    try {
      await loadNativeTrack(newTrack);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load audio';
      setErrorMessage(message);
    }
  }, []);

  const startQueue = useCallback<PlaybackContextValue['startQueue']>(
    async (orderedTracks, startIndex, options) => {
      setErrorMessage(null);
      try {
        await loadNativeQueue(orderedTracks, startIndex, options);
      } catch (err) {
        setErrorMessage(err instanceof Error ? err.message : 'Failed to load audio');
      }
    },
    [],
  );

  // Shuffle reorders only the upcoming tracks; the active track keeps playing
  // untouched. Best-effort: the store's play order is already updated, so a
  // failed native reorder just means the upcoming order lags until the next
  // queue rebuild.
  const reorderUpcoming = useCallback<PlaybackContextValue['reorderUpcoming']>(
    async (upcoming) => {
      try {
        await reorderUpcomingNative(upcoming);
      } catch {
        // native queue not ready — ignore
      }
    },
    [],
  );

  // Add to Queue / Play Next. Best-effort, same as reorderUpcoming: the store's
  // play order is already updated by useQueuePlayback, so a failed native add
  // only means the audio queue lags the UI until the next queue rebuild.
  const appendToQueue = useCallback<PlaybackContextValue['appendToQueue']>(async (track) => {
    try {
      await appendNativeTrack(track);
    } catch {
      // native queue not ready — ignore
    }
  }, []);

  const insertNext = useCallback<PlaybackContextValue['insertNext']>(async (track, position) => {
    try {
      await insertNativeTrackNext(track, position);
    } catch {
      // native queue not ready — ignore
    }
  }, []);

  // Native transitions: the next track is already buffered, so these are
  // instant and gapless. The store's currentIndex follows via the
  // PlaybackActiveTrackChanged listener in the playback service.
  const skipToQueueIndex = useCallback(async (index: number) => {
    try {
      await TrackPlayer.skip(index);
      await TrackPlayer.play();
    } catch {
      // index out of range / player not ready — ignore
    }
  }, []);

  const skipNext = useCallback(async () => {
    try { await TrackPlayer.skipToNext(); } catch { /* at end / not ready */ }
  }, []);

  const skipPrevious = useCallback(async () => {
    try { await TrackPlayer.skipToPrevious(); } catch { /* at start / not ready */ }
  }, []);

  const removeQueueIndex = useCallback(async (index: number) => {
    try { await TrackPlayer.remove(index); } catch { /* already gone / not ready */ }
  }, []);

  const pause = useCallback(() => { void TrackPlayer.pause(); }, []);
  const resume = useCallback(() => { void TrackPlayer.play(); }, []);
  const seekTo = useCallback((ms: number) => {
    void seekPreservingPlayback(ms / 1000, isPlayingRef.current);
  }, []);

  const stop = useCallback(() => {
    void TrackPlayer.reset();
    setTrack(null);
    setErrorMessage(null);
  }, []);

  const retry = useCallback(() => {
    // Rebuild the native queue at the current position if there is one;
    // otherwise replay the last standalone (preview) track.
    const s = useQueueStore.getState();
    if (s.currentTrack()) {
      void startQueue(orderedQueueTracks(s), s.currentIndex);
      return;
    }
    const trackToRetry = lastPlayedTrack.current;
    if (trackToRetry) void play(trackToRetry);
  }, [play, startQueue]);

  // Mirror the queue's repeat mode onto the native player so auto-advance and
  // repeat are enforced natively (no JS wake to load the next track).
  const repeatMode = useQueueStore((s) => s.repeatMode);
  useEffect(() => {
    void TrackPlayer.setRepeatMode(NATIVE_REPEAT[repeatMode]);
  }, [repeatMode]);

  const currentQueueTrack = useQueueStore((s) => s.currentTrack());

  // Follow the queue's current track. Only when there IS one: a queue-less track
  // (a preview via play()) owns `track` itself, and clearing it here on the
  // resulting null would blank the player mid-preview.
  useEffect(() => {
    if (!currentQueueTrack) return;
    setTrack(currentQueueTrack);
    setErrorMessage(null);
    lastPlayedTrack.current = currentQueueTrack;
  }, [currentQueueTrack]);

  useQueueResume();

  // Memoized so the frozen-position optimization above actually pays off: every
  // control below is a stable useCallback, so while backgrounded (positionMs
  // frozen => `state` identity stable) this value keeps its identity too and
  // useProgress's twice-a-second setState re-renders nothing downstream. A bare
  // object literal here would mint a new identity on every one of those ticks
  // and re-render every usePlayback() consumer in the tree regardless.
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
      stop,
      retry,
    ],
  );

  return <PlaybackContext.Provider value={value}>{children}</PlaybackContext.Provider>;
}
