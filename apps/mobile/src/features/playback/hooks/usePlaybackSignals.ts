/**
 * usePlaybackSignals — derives the play / skip / completed behavioral events
 * from live playback state, with listen duration (dwell_ms) and discovery
 * provenance.
 *
 * - play: the satisfaction signal, emitted once per track when the listen
 *   crosses 30s OR 50% of duration (whichever first). The funnel's start is the
 *   prior result_clicked, so there is no separate start event.
 * - completed: emitted once when the track reaches its end (play-to-completion,
 *   the strongest positive).
 * - skip: emitted for the OUTGOING track when the track changes before it
 *   completed, carrying how long it was actually listened (dwell_ms). A short
 *   dwell is the skip-after-click negative.
 *
 * All emission is fire-and-forget via the shared recordEvent hook and runs in
 * effects (never render), reading the live recordEvent + queue source through
 * refs so it adds no re-renders beyond the useProgress tick the provider already
 * runs.
 */

import { useEffect, useRef } from 'react';

import { useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackTrack } from '@shared/playback/types';
import { useRecordEvent } from '@shared/telemetry/useRecordEvent';

import { buildTrackPayload, hasCrossedListenThreshold, trackKey } from '../signals';

type SignalState = {
  track: PlaybackTrack | null;
  key: string | null;
  lastPositionMs: number;
  durationMs: number;
  playEmitted: boolean;
  completed: boolean;
};

export function usePlaybackSignals(args: {
  track: PlaybackTrack | null;
  positionMs: number;
  durationMs: number;
  isEnded: boolean;
}): void {
  const recordEvent = useRecordEvent();
  const recordRef = useRef(recordEvent);
  recordRef.current = recordEvent;
  const queueSource = useQueueStore((s) => s.source);
  const queueSourceRef = useRef(queueSource);
  queueSourceRef.current = queueSource;

  const stRef = useRef<SignalState>({
    track: null,
    key: null,
    lastPositionMs: 0,
    durationMs: 0,
    playEmitted: false,
    completed: false,
  });

  const emit = (
    type: 'play' | 'skip' | 'completed',
    track: PlaybackTrack,
    dwellMs?: number,
  ): void => {
    recordRef.current.mutate({
      type,
      search_id: track.searchId,
      payload: buildTrackPayload(track, queueSourceRef.current, dwellMs),
    });
  };

  // Track change → skip the outgoing track if it played but did not complete.
  const key = args.track ? trackKey(args.track) : null;
  useEffect(() => {
    const st = stRef.current;
    if (st.track !== null && !st.completed && st.lastPositionMs > 0 && st.key !== key) {
      emit('skip', st.track, st.lastPositionMs);
    }
    stRef.current = {
      track: args.track,
      key,
      lastPositionMs: 0,
      durationMs: args.durationMs,
      playEmitted: false,
      completed: false,
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- keyed on track identity only
  }, [key]);

  // Progress → keep dwell current; emit the satisfaction play once at threshold.
  useEffect(() => {
    const st = stRef.current;
    if (!st.track) return;
    st.lastPositionMs = args.positionMs;
    if (args.durationMs > 0) st.durationMs = args.durationMs;
    if (!st.playEmitted && hasCrossedListenThreshold(args.positionMs, st.durationMs)) {
      st.playEmitted = true;
      emit('play', st.track);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- effect reads refs + args directly
  }, [args.positionMs, args.durationMs]);

  // End → play-to-completion, once.
  useEffect(() => {
    const st = stRef.current;
    if (args.isEnded && st.track && !st.completed) {
      st.completed = true;
      emit('completed', st.track, st.durationMs || st.lastPositionMs);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- effect reads refs + args directly
  }, [args.isEnded]);
}
