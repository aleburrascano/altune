import { useEffect, useState } from 'react';
import { useProgress } from 'react-native-track-player';

import { useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackTrack } from '@shared/playback/types';

import { useIsForeground } from './useIsForeground';

export interface PlaybackPosition {
  /** Position shown to the UI: live, else the resume point, frozen while backgrounded. */
  positionMs: number;
  /** Raw native progress, unaffected by the resume point or the background freeze. */
  livePositionMs: number;
  /** Native duration, falling back to the track's metadata duration. */
  durationMs: number;
}

/**
 * Derives the playback position and duration from native progress, the queue's
 * persisted resume point, and app foreground state.
 */
export function usePlaybackPosition(track: PlaybackTrack | null): PlaybackPosition {
  const progress = useProgress(500);
  const isForeground = useIsForeground();

  const livePositionMs = progress.position * 1000;
  const resumePositionMs = useQueueStore((s) => s.resumePositionMs);
  const displayPositionMs = livePositionMs > 0 ? livePositionMs : resumePositionMs;
  useEffect(() => {
    if (livePositionMs > 0 && resumePositionMs > 0) {
      useQueueStore.getState().setResumePosition(0);
    }
  }, [livePositionMs, resumePositionMs]);
  // Freeze the last foreground position so backgrounding never surfaces a stale
  // or reset native progress. Captured when foreground flips off by adjusting
  // state during render, not by writing a ref in render (react-hooks/refs).
  const [frozenPositionMs, setFrozenPositionMs] = useState(0);
  const [wasForeground, setWasForeground] = useState(isForeground);
  if (wasForeground !== isForeground) {
    setWasForeground(isForeground);
    if (!isForeground) setFrozenPositionMs(displayPositionMs);
  }
  const positionMs = isForeground ? displayPositionMs : frozenPositionMs;
  const rawDurationMs = progress.duration * 1000;

  const trackDurationMs =
    track?.durationSeconds != null && Number.isFinite(track.durationSeconds)
      ? track.durationSeconds * 1000
      : 0;

  const durationMs = rawDurationMs || trackDurationMs;

  return { positionMs, livePositionMs, durationMs };
}
