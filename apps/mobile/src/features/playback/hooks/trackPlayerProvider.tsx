import { useEffect, useMemo, useState, type ReactNode } from 'react';
import TrackPlayer, { RepeatMode, State, usePlaybackState } from 'react-native-track-player';

import { onSignOut } from '@shared/session/signOutCleanup';
import { PlaybackContext } from '@shared/playback/PlaybackContext';
import { useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import type {
  PlaybackContextValue,
  PlaybackState,
  PlaybackTrack,
  RepeatMode as QueueRepeatMode,
} from '@shared/playback/types';

import {
  createNativePlaybackActions,
  ignoringNativeRejection,
} from '../createNativePlaybackActions';
import { derivePlaybackState } from '../derivePlaybackState';
import { ensurePlayerSetup } from '../initPlayer';
import { usePlaybackErrorFor } from '../playbackErrorStore';
import { usePlaybackPosition } from './usePlaybackPosition';
import { usePlaybackSignals } from './usePlaybackSignals';
import { useQueueResume } from './useQueueResume';

const NATIVE_REPEAT: Record<QueueRepeatMode, RepeatMode> = {
  off: RepeatMode.Off,
  all: RepeatMode.Queue,
  one: RepeatMode.Track,
};

export function TrackPlayerPlaybackProvider({ children }: { children: ReactNode }) {
  const [track, setTrack] = useState<PlaybackTrack | null>(null);
  const failure = usePlaybackErrorFor(track ? trackKey(track) : null);

  const playbackState = usePlaybackState();

  useEffect(() => {
    void ignoringNativeRejection(ensurePlayerSetup);
  }, []);

  const { positionMs, livePositionMs, durationMs } = usePlaybackPosition(track);

  const tpState = playbackState.state;
  const isPlaying = tpState === State.Playing;
  const isBuffering = tpState === State.Buffering || tpState === State.Loading;
  const isEnded = tpState === State.Ended;

  // Created once so the context's callbacks keep their identity across renders.
  const [native] = useState(() => createNativePlaybackActions(setTrack, isPlaying));
  useEffect(() => {
    native.syncIsPlaying(isPlaying);
  });

  const state: PlaybackState = useMemo(
    () =>
      derivePlaybackState({
        track,
        failure,
        isBuffering,
        isEnded,
        isPlaying,
        positionMs,
        durationMs,
      }),
    [track, failure, isEnded, isPlaying, isBuffering, positionMs, durationMs],
  );

  usePlaybackSignals({
    track,
    positionMs: livePositionMs,
    durationMs,
  });

  const repeatMode = useQueueStore((s) => s.repeatMode);
  useEffect(() => {
    void ignoringNativeRejection(async () => {
      await ensurePlayerSetup();
      await TrackPlayer.setRepeatMode(NATIVE_REPEAT[repeatMode]);
    });
  }, [repeatMode]);

  const currentQueueTrack = useQueueStore((s) => s.currentTrack());

  // Mirror the queue's active track into local state when it changes. Adjusting
  // state during render (tracking the previous value in state) avoids syncing
  // state from an effect (react-hooks/set-state-in-effect).
  const [syncedQueueTrack, setSyncedQueueTrack] = useState(currentQueueTrack);
  if (currentQueueTrack && currentQueueTrack !== syncedQueueTrack) {
    setSyncedQueueTrack(currentQueueTrack);
    setTrack(currentQueueTrack);
  }

  useEffect(() => {
    if (track) native.rememberTrack(track);
  }, [native, track]);

  // A direct account switch keeps this provider mounted; drop the displayed track so
  // the next user never sees the previous user's title/artwork (#827).
  useEffect(() => onSignOut(() => setTrack(null)), []);

  useQueueResume();

  const value = useMemo<PlaybackContextValue>(
    () => ({ ...state, ...native.controls }),
    [state, native],
  );

  return <PlaybackContext.Provider value={value}>{children}</PlaybackContext.Provider>;
}
