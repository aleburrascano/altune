import { useEffect, useState } from 'react';
import { useProgress } from 'react-native-track-player';

import { useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackTrack } from '@shared/playback/types';

import { useIsForeground } from '@shared/lifecycle';

import { progressSecondsToMs } from '../time';

export interface PlaybackPosition {
  positionMs: number;
  livePositionMs: number;
  durationMs: number;
}

export function usePlaybackPosition(track: PlaybackTrack | null): PlaybackPosition {
  const progress = useProgress(500);
  const isForeground = useIsForeground();

  const livePositionMs = progressSecondsToMs(progress.position);
  const resumePositionMs = useQueueStore((s) => s.resumePositionMs);
  const displayPositionMs = livePositionMs > 0 ? livePositionMs : resumePositionMs;
  useEffect(() => {
    if (livePositionMs > 0 && resumePositionMs > 0) {
      useQueueStore.getState().setResumePosition(0);
    }
  }, [livePositionMs, resumePositionMs]);
  const [frozenPositionMs, setFrozenPositionMs] = useState(0);
  const [wasForeground, setWasForeground] = useState(isForeground);
  if (wasForeground !== isForeground) {
    setWasForeground(isForeground);
    if (!isForeground) setFrozenPositionMs(displayPositionMs);
  }
  const positionMs = isForeground ? displayPositionMs : frozenPositionMs;
  const rawDurationMs = progressSecondsToMs(progress.duration);

  const trackDurationMs =
    track?.durationSeconds != null && Number.isFinite(track.durationSeconds)
      ? progressSecondsToMs(track.durationSeconds)
      : 0;

  const durationMs = rawDurationMs || trackDurationMs;

  return { positionMs, livePositionMs, durationMs };
}
