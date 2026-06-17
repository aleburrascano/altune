import { useAudioPlayer, useAudioPlayerStatus, setAudioModeAsync } from 'expo-audio';
import type { AudioSource } from 'expo-audio';
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useQueryClient } from '@tanstack/react-query';

import { PlaybackContext } from '@shared/playback/PlaybackContext';
import type { PlaybackContextValue, PlaybackState, PlaybackTrack } from '@shared/playback/types';

import { audioRequestHeaders, audioStreamUrl } from '../api/audio';

const INITIAL_STATE: PlaybackState = {
  status: 'idle',
  track: null,
  positionMs: 0,
  durationMs: 0,
  errorMessage: null,
};

const LOAD_TIMEOUT_MS = 15_000;
const RETRY_DELAY_MS = 1_000;
const MAX_RETRIES = 2;

export function PlaybackProvider({ children }: { children: ReactNode }) {
  const [track, setTrack] = useState<PlaybackTrack | null>(null);
  const [audioSource, setAudioSource] = useState<AudioSource | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const shouldAutoPlay = useRef(false);
  const lastPlayedTrack = useRef<PlaybackTrack | null>(null);
  const queryClient = useQueryClient();

  const player = useAudioPlayer(audioSource);
  const playerStatus = useAudioPlayerStatus(player);

  useEffect(() => {
    void setAudioModeAsync({
      shouldPlayInBackground: true,
      playsInSilentMode: true,
    });
  }, []);

  useEffect(() => {
    if (shouldAutoPlay.current && playerStatus.isLoaded && audioSource) {
      shouldAutoPlay.current = false;
      player.play();
      if (track) {
        player.setActiveForLockScreen(
          true,
          {
            title: track.title,
            artist: track.artist,
            artworkUrl: track.artworkUrl ?? undefined,
          },
          { showSeekForward: true, showSeekBackward: true },
        );
      }
    }
  }, [playerStatus.isLoaded, player, audioSource, track]);

  useEffect(() => {
    if (!shouldAutoPlay.current || !track) return;
    const timeout = setTimeout(() => {
      if (shouldAutoPlay.current) {
        shouldAutoPlay.current = false;
        setErrorMessage('Audio is taking too long to load. Check your connection and try again.');
      }
    }, LOAD_TIMEOUT_MS);
    return () => clearTimeout(timeout);
  }, [track]);

  useEffect(() => {
    if (
      track?.source.kind === 'preview' &&
      !playerStatus.playing &&
      playerStatus.isLoaded &&
      playerStatus.duration > 0 &&
      playerStatus.currentTime >= playerStatus.duration
    ) {
      player.pause();
      player.seekTo(0);
    }
  }, [track, playerStatus.playing, playerStatus.isLoaded, playerStatus.currentTime, playerStatus.duration, player]);

  const safeMs = (seconds: number | undefined | null): number => {
    if (seconds == null || !Number.isFinite(seconds) || seconds < 0) return 0;
    return seconds * 1000;
  };

  const rawDuration = playerStatus.duration;
  const rawPosition = playerStatus.currentTime;

  let fallbackDurationMs = 0;
  if (track != null && track.source.kind === 'library') {
    const trackId = track.source.trackId;
    const homeData = queryClient.getQueryData<{ items: Array<{ id: string; duration_seconds: number | null }> }>(['library-home']);
    const match = homeData?.items.find((t) => t.id === trackId);
    if (match?.duration_seconds != null && Number.isFinite(match.duration_seconds)) {
      fallbackDurationMs = match.duration_seconds * 1000;
    }
  }
  if (fallbackDurationMs === 0 && track?.durationSeconds != null && Number.isFinite(track.durationSeconds)) {
    fallbackDurationMs = track.durationSeconds * 1000;
  }

  const durationMs = safeMs(rawDuration) || fallbackDurationMs;
  const positionMs = safeMs(rawPosition);

  const isEnded =
    track != null &&
    !playerStatus.playing &&
    playerStatus.isLoaded &&
    durationMs > 0 &&
    positionMs >= durationMs;

  const state: PlaybackState = useMemo(() => {
    if (!track) return INITIAL_STATE;
    if (errorMessage) return { status: 'error', track, positionMs: 0, durationMs: 0, errorMessage };

    if (shouldAutoPlay.current || (playerStatus.isBuffering && !playerStatus.isLoaded)) {
      return { status: 'loading', track, positionMs: 0, durationMs, errorMessage: null };
    }

    if (isEnded) {
      return {
        status: 'ended',
        track,
        positionMs: durationMs,
        durationMs,
        errorMessage: null,
      };
    }

    const status: PlaybackState['status'] = playerStatus.playing ? 'playing' : 'paused';

    return {
      status,
      track,
      positionMs,
      durationMs,
      errorMessage: null,
    };
  }, [track, errorMessage, isEnded, positionMs, durationMs, playerStatus.playing, playerStatus.isBuffering, playerStatus.isLoaded]);

  const loadAudioSource = useCallback(async (trackToLoad: PlaybackTrack, attempt: number): Promise<void> => {
    try {
      if (trackToLoad.source.kind === 'preview') {
        setAudioSource({ uri: trackToLoad.source.previewUrl });
      } else {
        const headers = await audioRequestHeaders();
        setAudioSource({ uri: audioStreamUrl(trackToLoad.source.trackId), headers });
      }
    } catch (err) {
      if (attempt < MAX_RETRIES) {
        await new Promise((resolve) => { setTimeout(resolve, RETRY_DELAY_MS); });
        return loadAudioSource(trackToLoad, attempt + 1);
      }
      shouldAutoPlay.current = false;
      const message = err instanceof Error ? err.message : 'Failed to load audio';
      setErrorMessage(message);
    }
  }, []);

  const play = useCallback(async (newTrack: PlaybackTrack) => {
    player.pause();
    setErrorMessage(null);
    setTrack(newTrack);
    setAudioSource(null);
    shouldAutoPlay.current = true;
    lastPlayedTrack.current = newTrack;
    await loadAudioSource(newTrack, 0);
  }, [player, loadAudioSource]);

  const pause = useCallback(() => {
    player.pause();
  }, [player]);

  const resume = useCallback(() => {
    player.play();
  }, [player]);

  const seekTo = useCallback((positionMs: number) => {
    player.seekTo(positionMs / 1000);
  }, [player]);

  const stop = useCallback(() => {
    player.pause();
    player.seekTo(0);
    player.clearLockScreenControls();
    setTrack(null);
    setAudioSource(null);
    setErrorMessage(null);
  }, [player]);

  const retry = useCallback(() => {
    const trackToRetry = lastPlayedTrack.current;
    if (!trackToRetry) return;
    setErrorMessage(null);
    setAudioSource(null);
    shouldAutoPlay.current = true;
    void loadAudioSource(trackToRetry, 0);
  }, [loadAudioSource]);

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
