import { useAudioPlayer, useAudioPlayerStatus, setAudioModeAsync } from 'expo-audio';
import type { AudioSource } from 'expo-audio';
import { createContext, useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';

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

  const player = useAudioPlayer(audioSource);
  const playerStatus = useAudioPlayerStatus(player);

  useEffect(() => {
    void setAudioModeAsync({
      shouldPlayInBackground: true,
      playsInSilentMode: true,
    });
  }, []);

  const state: PlaybackState = useMemo(() => {
    if (!track) return INITIAL_STATE;
    if (errorMessage) return { status: 'error', track, positionMs: 0, durationMs: 0, errorMessage };

    let status: PlaybackState['status'] = 'idle';
    if (playerStatus.playing) status = 'playing';
    else if (playerStatus.isBuffering && !playerStatus.isLoaded) status = 'loading';
    else if (track) status = 'paused';

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
    try {
      const headers = await audioRequestHeaders();
      const source: AudioSource = { uri: audioStreamUrl(newTrack.trackId), headers };
      setAudioSource(source);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load audio';
      setErrorMessage(message);
    }
  }, []);

  useEffect(() => {
    if (audioSource && track) {
      player.play();
      if (track) {
        player.setActiveForLockScreen(true, {
          title: track.title,
          artist: track.artist,
          artworkUrl: track.artworkUrl ?? undefined,
        });
      }
    }
  }, [audioSource]);

  const pause = useCallback(async () => {
    player.pause();
  }, [player]);

  const resume = useCallback(async () => {
    player.play();
  }, [player]);

  const seekTo = useCallback(async (positionMs: number) => {
    player.seekTo(positionMs / 1000);
  }, [player]);

  const stop = useCallback(async () => {
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
