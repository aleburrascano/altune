import { useEffect, useRef } from 'react';
import { Event, useTrackPlayerEvents } from 'react-native-track-player';

import { useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackTrack } from '@shared/playback/types';
import { useRecordEvent } from '@shared/telemetry/useRecordEvent';

import { buildTrackPayload, hasCrossedListenThreshold, trackKey } from '../signals';

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

  const { track, positionMs, durationMs } = args;
  const playRef = useRef<{ key: string | null; emitted: boolean }>({ key: null, emitted: false });
  const key = track ? trackKey(track) : null;
  useEffect(() => {
    playRef.current = { key, emitted: false };
  }, [key]);
  useEffect(() => {
    const ps = playRef.current;
    if (!track) return;
    if (!ps.emitted && hasCrossedListenThreshold(positionMs, durationMs)) {
      ps.emitted = true;
      emitRef.current('play', track);
    }
  }, [track, positionMs, durationMs]);

  const handledKeyRef = useRef<string | null>(null);
  useTrackPlayerEvents([Event.PlaybackActiveTrackChanged, Event.PlaybackQueueEnded], (event) => {
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
      const trackIdx = s.playOrder[event.track];
      const ended = trackIdx != null ? s.tracks[trackIdx] : undefined;
      if (!ended) return;
      if (handledKeyRef.current === trackKey(ended)) return;
      const dwellMs = Math.round((event.position ?? 0) * 1000) || undefined;
      emitRef.current('completed', ended, dwellMs);
    }
  });
}
