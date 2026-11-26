import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';

import { onSignOut } from '@shared/session';
import {
  type PlaybackContextValue,
  type PlaybackState,
  PlaybackContext,
  useQueueStore,
  trackKey,
  type PlaybackTrack,
} from '@shared/playback';
import { derivePlaybackState } from '@features/player/domain/derivePlaybackState';
import { useNativePlayback } from '@features/player/view/NativePlaybackProvider';
import { usePlaybackErrorFor } from '@features/player/view/usePlaybackErrorFor';
import { usePlaybackPosition } from '@features/player/view/usePlaybackPosition';
import { usePlaybackSignals } from '@features/player/view/usePlaybackSignals';
import { useQueueResume } from '@features/player/view/useQueueResume';

interface DisplayedTrack {
  track: PlaybackTrack | null;
  syncedQueueTrack: PlaybackTrack | null;
}

export function TrackPlayerPlaybackProvider({ children }: { children: ReactNode }) {
  const playback = useNativePlayback();
  const [displayed, setDisplayed] = useState<DisplayedTrack>(() => ({
    track: null,
    syncedQueueTrack: useQueueStore.getState().currentTrack(),
  }));
  const { track, syncedQueueTrack } = displayed;
  const setTrack = useCallback(
    (next: PlaybackTrack | null) => setDisplayed((prev) => ({ ...prev, track: next })),
    [],
  );
  const failure = usePlaybackErrorFor(track ? trackKey(track) : null);

  const phase = playback.usePhase();

  useEffect(() => {
    void playback.ensureSetup();
  }, [playback]);

  const { positionMs, livePositionMs, durationMs } = usePlaybackPosition(track);

  const isPlaying = phase === 'playing';

  const [native] = useState(() => playback.createActions(setTrack, isPlaying));
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
    void playback.applyRepeatMode(repeatMode);
  }, [playback, repeatMode]);

  const currentQueueTrack = useQueueStore((s) => s.currentTrack());

  if (currentQueueTrack && currentQueueTrack !== syncedQueueTrack) {
    native.rememberTrack(currentQueueTrack);
    setDisplayed({ track: currentQueueTrack, syncedQueueTrack: currentQueueTrack });
  }

  useEffect(() => onSignOut(() => setTrack(null)), [setTrack]);

  useQueueResume();

  const value = useMemo<PlaybackContextValue>(
    () => ({ ...state, ...native.controls }),
    [state, native],
  );

  return <PlaybackContext.Provider value={value}>{children}</PlaybackContext.Provider>;
}
