import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import TrackPlayer, {
  State,
  usePlaybackState,
  useProgress,
} from 'react-native-track-player';

import { PlaybackContext } from '@shared/playback/PlaybackContext';
import { useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackContextValue, PlaybackState, PlaybackTrack } from '@shared/playback/types';

import { ensurePlayerSetup } from '../initPlayer';
import { loadNativeTrack } from '../loadNativeTrack';
import { useIsForeground } from './useIsForeground';
import { useQueueResume } from './useQueueResume';

// AIDEV-NOTE: The real, track-player-backed playback provider. It is imported
// ONLY outside Expo Go (via the PlaybackProvider selector), because the
// top-level `react-native-track-player` import touches a native module that
// Expo Go does not bundle. In Expo Go, expoGoPlaybackProvider is used instead.

const INITIAL_STATE: PlaybackState = {
  status: 'idle',
  track: null,
  positionMs: 0,
  durationMs: 0,
  errorMessage: null,
};

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
  if (isForeground) frozenPositionMs.current = livePositionMs;
  const positionMs = isForeground ? livePositionMs : frozenPositionMs.current;
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

  const state: PlaybackState = useMemo(() => {
    if (!track) return INITIAL_STATE;
    if (errorMessage) return { status: 'error', track, positionMs: 0, durationMs: 0, errorMessage };
    if (isBuffering) return { status: 'loading', track, positionMs: 0, durationMs, errorMessage: null };
    if (isEnded) return { status: 'ended', track, positionMs: durationMs, durationMs, errorMessage: null };

    return {
      status: isPlaying ? 'playing' : 'paused',
      track,
      positionMs,
      durationMs,
      errorMessage: null,
    };
  }, [track, errorMessage, isEnded, isPlaying, isBuffering, positionMs, durationMs]);

  const play = useCallback(async (newTrack: PlaybackTrack) => {
    setErrorMessage(null);
    setTrack(newTrack);
    lastPlayedTrack.current = newTrack;

    try {
      await loadNativeTrack(newTrack);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load audio';
      setErrorMessage(message);
    }
  }, []);

  const pause = useCallback(() => { void TrackPlayer.pause(); }, []);
  const resume = useCallback(() => { void TrackPlayer.play(); }, []);
  const seekTo = useCallback((ms: number) => { void TrackPlayer.seekTo(ms / 1000); }, []);

  const stop = useCallback(() => {
    void TrackPlayer.reset();
    setTrack(null);
    setErrorMessage(null);
  }, []);

  const retry = useCallback(() => {
    const trackToRetry = lastPlayedTrack.current;
    if (!trackToRetry) return;
    void play(trackToRetry);
  }, [play]);

  const currentQueueTrack = useQueueStore((s) => s.currentTrack());

  useEffect(() => {
    setTrack(currentQueueTrack);
    if (currentQueueTrack) {
      setErrorMessage(null);
      lastPlayedTrack.current = currentQueueTrack;
    }
  }, [currentQueueTrack]);

  useQueueResume();

  const value: PlaybackContextValue = {
    ...state,
    play,
    pause,
    resume,
    seekTo,
    stop,
    retry,
  };

  return <PlaybackContext.Provider value={value}>{children}</PlaybackContext.Provider>;
}
