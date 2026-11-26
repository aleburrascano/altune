import { useEffect, useEffectEvent, useRef } from 'react';

import { useQueueStore, trackKey, type TrackKey, type PlaybackTrack } from '@shared/playback';
import { useRecordEvent } from '@shared/telemetry';

import { buildTrackPayload, hasCrossedListenThreshold } from '@features/player/domain/signals';
import { progressSecondsToMs, progressSecondsToRoundedMs } from '@features/player/domain/time';
import { useNativePlayback } from '@features/player/view/NativePlaybackProvider';

const COMPLETION_EPSILON_MS = 2000;

type PlayProgress = { key: TrackKey | null; emitted: boolean; stalePositionMs: number };

function durationMsOf(
  reportedSeconds: number | null | undefined,
  knownSeconds: number | null | undefined,
): number {
  const seconds = reportedSeconds ?? knownSeconds;
  return seconds != null ? progressSecondsToMs(seconds) : 0;
}

function isFirstCrossing(ps: PlayProgress, positionMs: number, durationMs: number): boolean {
  if (ps.emitted || positionMs === ps.stalePositionMs) return false;
  return hasCrossedListenThreshold(positionMs, durationMs);
}

export function usePlaybackSignals(args: {
  track: PlaybackTrack | null;
  positionMs: number;
  durationMs: number;
}): void {
  const recordEvent = useRecordEvent();
  const queueSource = useQueueStore((s) => s.source);

  const emit = (
    type: 'play' | 'skip' | 'completed',
    track: PlaybackTrack,
    dwellMs?: number,
  ): void => {
    recordEvent.mutate({
      type,
      search_id: track.searchId,
      payload: buildTrackPayload(track, queueSource, dwellMs),
    });
  };

  const { track, positionMs, durationMs } = args;
  const playRef = useRef<PlayProgress>({ key: null, emitted: false, stalePositionMs: 0 });
  const key = track ? trackKey(track) : null;
  const trackProgress = useEffectEvent((): void => {
    const ps = playRef.current;
    if (ps.key !== key) {
      playRef.current = { key, emitted: false, stalePositionMs: positionMs };
      return;
    }
    if (!track || !isFirstCrossing(ps, positionMs, durationMs)) return;
    ps.emitted = true;
    emit('play', track);
  });
  useEffect(() => {
    trackProgress();
  }, [track, key, positionMs, durationMs]);

  const handledKeyRef = useRef<TrackKey | null>(null);
  useNativePlayback().useTransitions((event) => {
    const s = useQueueStore.getState();
    if (event.kind === 'trackChanged') {
      if (event.lastIndex == null) return;
      const trackIdx = s.playOrder[event.lastIndex];
      const outgoing = trackIdx != null ? s.tracks[trackIdx] : undefined;
      if (!outgoing) return;
      const dwellMs = progressSecondsToRoundedMs(event.lastPositionSeconds ?? 0);
      if (dwellMs <= 0) return;
      const durMs = durationMsOf(event.lastTrackDurationSeconds, outgoing.durationSeconds);
      const completed = durMs > 0 && dwellMs >= durMs - COMPLETION_EPSILON_MS;
      handledKeyRef.current = trackKey(outgoing);
      emit(completed ? 'completed' : 'skip', outgoing, dwellMs);
    } else {
      const trackIdx = s.playOrder[event.index];
      const ended = trackIdx != null ? s.tracks[trackIdx] : undefined;
      if (!ended) return;
      const alreadyHandled = handledKeyRef.current === trackKey(ended);
      handledKeyRef.current = null;
      if (alreadyHandled) return;
      const dwellMs = progressSecondsToRoundedMs(event.positionSeconds ?? 0) || undefined;
      emit('completed', ended, dwellMs);
    }
  });
}
