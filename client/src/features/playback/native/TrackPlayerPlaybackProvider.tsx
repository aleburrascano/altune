import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import TrackPlayer, { RepeatMode, State, usePlaybackState } from 'react-native-track-player';

import { onSignOut } from '@shared/session/signOutCleanup';
import { PlaybackContext } from '@shared/playback/usePlayback';
import { useQueueStore } from '@shared/playback/queueStore';
import { trackKey, type PlaybackTrack } from '@shared/playback/playbackTrack';
import type { PlaybackContextValue, PlaybackState } from '@shared/playback/playbackContextValue';
import type { RepeatMode as QueueRepeatMode } from '@shared/playback/queueOrder';

import {
  createNativePlaybackActions,
  ignoringNativeRejection,
} from '@features/playback/native/createNativePlaybackActions';
import {
  derivePlaybackState,
  type PlayerPhase,
} from '@features/playback/domain/derivePlaybackState';
import { ensurePlayerSetup } from '@features/playback/native/initPlayer';
import { usePlaybackErrorFor } from '@features/playback/hooks/usePlaybackErrorStore';
import { usePlaybackPosition } from '@features/playback/hooks/usePlaybackPosition';
import { usePlaybackSignals } from '@features/playback/hooks/usePlaybackSignals';
import { useQueueResume } from '@features/playback/hooks/useQueueResume';

const NATIVE_REPEAT: Record<QueueRepeatMode, RepeatMode> = {
  off: RepeatMode.Off,
  all: RepeatMode.Queue,
  one: RepeatMode.Track,
};

function phaseFromTrackPlayerState(tpState: State | undefined): PlayerPhase {
  if (tpState === State.Buffering || tpState === State.Loading) return 'loading';
  if (tpState === State.Ended) return 'ended';
  return tpState === State.Playing ? 'playing' : 'paused';
}

export function TrackPlayerPlaybackProvider({ children }: { children: ReactNode }) {
  const [displayed, setDisplayed] = useState<{
    track: PlaybackTrack | null;
    syncedQueueTrack: PlaybackTrack | null;
  }>(() => ({ track: null, syncedQueueTrack: useQueueStore.getState().currentTrack() }));
  const { track, syncedQueueTrack } = displayed;
  const setTrack = useCallback(
    (next: PlaybackTrack | null) => setDisplayed((prev) => ({ ...prev, track: next })),
    [],
  );
  const failure = usePlaybackErrorFor(track ? trackKey(track) : null);

  const playbackState = usePlaybackState();

  useEffect(() => {
    void ignoringNativeRejection(ensurePlayerSetup);
  }, []);

  const { positionMs, livePositionMs, durationMs } = usePlaybackPosition(track);

  const tpState = playbackState.state;
  const isPlaying = tpState === State.Playing;
  const phase = phaseFromTrackPlayerState(tpState);

  const [native] = useState(() => createNativePlaybackActions(setTrack, isPlaying));
  useEffect(() => {
    native.syncIsPlaying(isPlaying);
  });

  const state: PlaybackState = useMemo(
    () =>
      derivePlaybackState({
        track,
        failure,
        phase,
        positionMs,
        durationMs,
      }),
    [track, failure, phase, positionMs, durationMs],
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

  if (currentQueueTrack && currentQueueTrack !== syncedQueueTrack) {
    setDisplayed({ track: currentQueueTrack, syncedQueueTrack: currentQueueTrack });
  }

  useEffect(() => {
    if (track) native.rememberTrack(track);
  }, [native, track]);

  useEffect(() => onSignOut(() => setTrack(null)), [setTrack]);

  useQueueResume();

  const value = useMemo<PlaybackContextValue>(
    () => ({ ...state, ...native.controls }),
    [state, native],
  );

  return <PlaybackContext.Provider value={value}>{children}</PlaybackContext.Provider>;
}
