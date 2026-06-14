import { useAudioPlayer, useAudioPlayerStatus, setAudioModeAsync } from 'expo-audio';
import type { AudioSource } from 'expo-audio';
import { createContext, useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useQueryClient } from '@tanstack/react-query';

import { audioRequestHeaders, audioStreamUrl } from '../api/audio';
import type { PlaybackContextValue, PlaybackState, PlaybackTrack } from '../types';

const INITIAL_STATE: PlaybackState = {
  status: 'idle',
  track: null,
  positionMs: 0,
  durationMs: 0,
  errorMessage: null,
};

export const PlaybackContext = createContext<PlaybackContextValue | null>(null);

export function PlaybackProvider({ children }: { children: ReactNode }) {
  const [track, setTrack] = useState<PlaybackTrack | null>(null);
  const [audioSource, setAudioSource] = useState<AudioSource | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const shouldAutoPlay = useRef(false);
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
    }
  }, [playerStatus.isLoaded, player, audioSource]);

  useEffect(() => {
    if (!shouldAutoPlay.current || !track) return;
    const timeout = setTimeout(() => {
      if (shouldAutoPlay.current) {
        shouldAutoPlay.current = false;
        setErrorMessage('Could not load audio — the file may be missing');
        void queryClient.invalidateQueries({ queryKey: ['library-home'] });
        void queryClient.invalidateQueries({ queryKey: ['library'] });
      }
    }, 10000);
    return () => clearTimeout(timeout);
  }, [track, queryClient]);

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

  const state: PlaybackState = useMemo(() => {
    if (!track) return INITIAL_STATE;
    if (errorMessage) return { status: 'error', track, positionMs: 0, durationMs: 0, errorMessage };
    if (shouldAutoPlay.current || (playerStatus.isBuffering && !playerStatus.isLoaded)) {
      return { status: 'loading', track, positionMs: 0, durationMs: 0, errorMessage: null };
    }

    const status: PlaybackState['status'] = playerStatus.playing ? 'playing' : 'paused';

    return {
      status,
      track,
      positionMs: (playerStatus.currentTime ?? 0) * 1000,
      durationMs: (playerStatus.duration ?? 0) * 1000,
      errorMessage: null,
    };
  }, [track, errorMessage, playerStatus.playing, playerStatus.isBuffering, playerStatus.isLoaded, playerStatus.currentTime, playerStatus.duration]);

  const play = useCallback(async (newTrack: PlaybackTrack) => {
    setErrorMessage(null);
    setTrack(newTrack);
    setAudioSource(null);
    shouldAutoPlay.current = true;
    try {
      if (newTrack.source.kind === 'preview') {
        setAudioSource({ uri: newTrack.source.previewUrl });
      } else {
        const headers = await audioRequestHeaders();
        setAudioSource({ uri: audioStreamUrl(newTrack.source.trackId), headers });
      }
    } catch (err) {
      shouldAutoPlay.current = false;
      const message = err instanceof Error ? err.message : 'Failed to load audio';
      setErrorMessage(message);
    }
  }, []);

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
    setTrack(null);
    setAudioSource(null);
    setErrorMessage(null);
  }, [player]);

  const value: PlaybackContextValue = {
    ...state,
    play,
    pause,
    resume,
    seekTo,
    stop,
  };

  return <PlaybackContext.Provider value={value}>{children}</PlaybackContext.Provider>;
}
