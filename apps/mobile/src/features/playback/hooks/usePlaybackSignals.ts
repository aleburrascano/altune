/**
 * usePlaybackSignals — derives the play / skip / completed behavioral events
 * with listen duration (dwell_ms) and discovery provenance.
 *
 * - play: the satisfaction signal, emitted once per track when the listen
 *   crosses 30s OR 50% of duration (whichever first). Position-driven. The
 *   funnel's start is the prior result_clicked, so there is no separate start.
 * - completed / skip: derived from native queue *transitions*
 *   (PlaybackActiveTrackChanged / PlaybackQueueEnded), not State.Ended — under
 *   a gapless native queue, State.Ended only fires at queue end, so per-track
 *   completion must come from the transition's outgoing track + its last
 *   position. An outgoing track that stopped near its end is `completed`; one
 *   cut short is a `skip` carrying how long it was actually heard (dwell_ms).
 *
 * All emission is fire-and-forget via the shared recordEvent hook. The outgoing
 * track is looked up from the queue store by the event's lastIndex so it still
 * carries searchId / resultSignature provenance.
 */

import { useEffect, useRef } from 'react';
import { Event, useTrackPlayerEvents } from 'react-native-track-player';

import { useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackTrack } from '@shared/playback/types';
import { useRecordEvent } from '@shared/telemetry/useRecordEvent';

import { buildTrackPayload, hasCrossedListenThreshold, trackKey } from '../signals';

// A track that stopped within this window of its end counts as played-to-end
// (durations from metadata are rarely exact to the millisecond).
const COMPLETION_EPSILON_MS = 2000;

export function usePlaybackSignals(args: {
  track: PlaybackTrack | null;
  positionMs: number;
  durationMs: number;
}): void {
  const recordEvent = useRecordEvent();
  const recordRef = useRef(recordEvent);
  recordRef.current = recordEvent;
  const queueSource = useQueueStore((s) => s.source);
  const queueSourceRef = useRef(queueSource);
  queueSourceRef.current = queueSource;

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
  const emitRef = useRef(emit);
  emitRef.current = emit;

  // play — satisfaction threshold, once per track. Reset the latch on track id.
  const playRef = useRef<{ key: string | null; emitted: boolean }>({ key: null, emitted: false });
  const key = args.track ? trackKey(args.track) : null;
  useEffect(() => {
    playRef.current = { key, emitted: false };
  }, [key]);
  useEffect(() => {
    const ps = playRef.current;
    if (!args.track) return;
    if (!ps.emitted && hasCrossedListenThreshold(args.positionMs, args.durationMs)) {
      ps.emitted = true;
      emitRef.current('play', args.track);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- reads refs + args directly
  }, [args.positionMs, args.durationMs, key]);

  // skip / completed — derived from native transitions so they survive gapless
  // auto-advance. handledKey dedups the final track between a wrap-around
  // PlaybackActiveTrackChanged and a queue-end PlaybackQueueEnded.
  const handledKeyRef = useRef<string | null>(null);
  useTrackPlayerEvents(
    [Event.PlaybackActiveTrackChanged, Event.PlaybackQueueEnded],
    (event) => {
      const s = useQueueStore.getState();
      if (event.type === Event.PlaybackActiveTrackChanged) {
        if (event.lastIndex == null) return;
        const trackIdx = s.playOrder[event.lastIndex];
        const outgoing = trackIdx != null ? s.tracks[trackIdx] : undefined;
        if (!outgoing) return;
        const dwellMs = Math.round((event.lastPosition ?? 0) * 1000);
        if (dwellMs <= 0) return;
        const durMs =
          event.lastTrack?.duration != null
            ? event.lastTrack.duration * 1000
            : outgoing.durationSeconds != null
              ? outgoing.durationSeconds * 1000
              : 0;
        const completed = durMs > 0 && dwellMs >= durMs - COMPLETION_EPSILON_MS;
        handledKeyRef.current = trackKey(outgoing);
        emitRef.current(completed ? 'completed' : 'skip', outgoing, dwellMs);
      } else {
        // PlaybackQueueEnded — the last track played to completion.
        const trackIdx = s.playOrder[event.track];
        const ended = trackIdx != null ? s.tracks[trackIdx] : undefined;
        if (!ended) return;
        if (handledKeyRef.current === trackKey(ended)) return;
        const dwellMs = Math.round((event.position ?? 0) * 1000) || undefined;
        emitRef.current('completed', ended, dwellMs);
      }
    },
  );
}
