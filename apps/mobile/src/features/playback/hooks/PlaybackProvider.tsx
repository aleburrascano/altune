import { Audio, type AVPlaybackStatus } from 'expo-av';
import { createContext, useCallback, useEffect, useRef, useState, type ReactNode } from 'react';

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
  const soundRef = useRef<Audio.Sound | null>(null);
  const [state, setState] = useState<PlaybackState>(INITIAL_STATE);

  useEffect(() => {
    void Audio.setAudioModeAsync({
      staysActiveInBackground: true,
      playsInSilentModeIOS: true,
    });
    return () => {
      if (soundRef.current) {
        void soundRef.current.unloadAsync();
      }
    };
  }, []);

  const onPlaybackStatusUpdate = useCallback((status: AVPlaybackStatus) => {
    if (!status.isLoaded) {
      if (status.error) {
        setState((prev) => ({ ...prev, status: 'error', errorMessage: status.error ?? 'Playback error' }));
      }
      return;
    }
    setState((prev) => ({
      ...prev,
      status: status.isPlaying ? 'playing' : 'paused',
      positionMs: status.positionMillis,
      durationMs: status.durationMillis ?? 0,
      errorMessage: null,
    }));
  }, []);

  const play = useCallback(async (track: PlaybackTrack) => {
    if (soundRef.current) {
      await soundRef.current.unloadAsync();
    }
    setState((prev) => ({
      ...prev,
      status: 'loading',
      track,
      positionMs: 0,
      durationMs: 0,
      errorMessage: null,
    }));
    try {
      const headers = await audioRequestHeaders();
      const { sound } = await Audio.Sound.createAsync(
        { uri: audioStreamUrl(track.trackId), headers },
        { shouldPlay: true },
        onPlaybackStatusUpdate,
      );
      soundRef.current = sound;
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load audio';
      setState((prev) => ({ ...prev, status: 'error', errorMessage: message }));
    }
  }, [onPlaybackStatusUpdate]);

  const pause = useCallback(async () => {
    if (soundRef.current) {
      await soundRef.current.pauseAsync();
    }
  }, []);

  const resume = useCallback(async () => {
    if (soundRef.current) {
      await soundRef.current.playAsync();
    }
  }, []);

  const seekTo = useCallback(async (positionMs: number) => {
    if (soundRef.current) {
      await soundRef.current.setPositionAsync(positionMs);
    }
  }, []);

  const stop = useCallback(async () => {
    if (soundRef.current) {
      await soundRef.current.unloadAsync();
      soundRef.current = null;
    }
    setState(INITIAL_STATE);
  }, []);

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
