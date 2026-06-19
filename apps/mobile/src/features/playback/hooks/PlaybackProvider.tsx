import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import TrackPlayer, {
  Capability,
  Event,
  State,
  usePlaybackState,
  useProgress,
} from 'react-native-track-player';

import { PlaybackContext } from '@shared/playback/PlaybackContext';
import { useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackContextValue, PlaybackState, PlaybackTrack } from '@shared/playback/types';

import { audioRequestHeaders, audioStreamUrl } from '../api/audio';
import { useQueueResume } from './useQueueResume';

const INITIAL_STATE: PlaybackState = {
  status: 'idle',
  track: null,
  positionMs: 0,
  durationMs: 0,
  errorMessage: null,
};

let playerInitialized = false;

async function initPlayer(): Promise<void> {
  if (playerInitialized) return;
  await TrackPlayer.setupPlayer({});
  await TrackPlayer.updateOptions({
    capabilities: [
      Capability.Play,
      Capability.Pause,
      Capability.SeekTo,
      Capability.SkipToNext,
      Capability.SkipToPrevious,
    ],
    compactCapabilities: [
      Capability.Play,
      Capability.Pause,
      Capability.SkipToNext,
    ],
  });
  playerInitialized = true;
}

export function PlaybackProvider({ children }: { children: ReactNode }) {
  const [track, setTrack] = useState<PlaybackTrack | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [ready, setReady] = useState(false);
  const lastPlayedTrack = useRef<PlaybackTrack | null>(null);
  const queryClient = useQueryClient();

  const playbackState = usePlaybackState();
  const progress = useProgress(500);

  useEffect(() => {
    void initPlayer().then(() => setReady(true));
  }, []);

  const positionMs = progress.position * 1000;
  const rawDurationMs = progress.duration * 1000;

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

  const durationMs = rawDurationMs || fallbackDurationMs;

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
      await TrackPlayer.reset();
      const artwork = newTrack.artworkUrl ?? '';
      if (newTrack.source.kind === 'preview') {
        await TrackPlayer.load({
          url: newTrack.source.previewUrl,
          title: newTrack.title,
          artist: newTrack.artist,
          artwork,
        });
      } else {
        const headers = await audioRequestHeaders();
        await TrackPlayer.load({
          url: audioStreamUrl(newTrack.source.trackId),
          title: newTrack.title,
          artist: newTrack.artist,
          artwork,
          headers,
        });
      }
      await TrackPlayer.play();
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

  // Sync Zustand from TrackPlayer's active track changes (native queue transitions)
  useEffect(() => {
    if (!ready) return;
    const sub = TrackPlayer.addEventListener(Event.PlaybackActiveTrackChanged, (data) => {
      if (data.index == null) return;
      const { tracks, playOrder, currentIndex } = useQueueStore.getState();
      if (tracks.length === 0) return;

      // Find which queue position this native index corresponds to
      const queueIdx = playOrder.indexOf(data.index);
      if (queueIdx >= 0 && queueIdx !== currentIndex) {
        useQueueStore.getState().skipToIndex(queueIdx);
      }

      // Update the displayed track
      const playbackTrack = tracks[data.index];
      if (playbackTrack) {
        setTrack(playbackTrack);
        setErrorMessage(null);
        lastPlayedTrack.current = playbackTrack;
      }
    });
    return () => sub.remove();
  }, [ready]);

  useEffect(() => {
    if (!ready) return;
    const remotePlay = TrackPlayer.addEventListener(Event.RemotePlay, () => { void TrackPlayer.play(); });
    const remotePause = TrackPlayer.addEventListener(Event.RemotePause, () => { void TrackPlayer.pause(); });
    const remoteNext = TrackPlayer.addEventListener(Event.RemoteNext, () => {
      const nextTrack = useQueueStore.getState().skipToNext();
      if (nextTrack) void TrackPlayer.skipToNext();
    });
    const remotePrev = TrackPlayer.addEventListener(Event.RemotePrevious, () => {
      const prevTrack = useQueueStore.getState().skipToPrevious();
      if (prevTrack) void TrackPlayer.skipToPrevious();
    });
    const remoteSeek = TrackPlayer.addEventListener(Event.RemoteSeek, (data) => {
      void TrackPlayer.seekTo(data.position);
    });

    return () => {
      remotePlay.remove();
      remotePause.remove();
      remoteNext.remove();
      remotePrev.remove();
      remoteSeek.remove();
    };
  }, [ready]);

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
