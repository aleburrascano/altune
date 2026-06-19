import { useCallback, useEffect, useRef } from 'react';
import { AppState } from 'react-native';
import { useQueryClient } from '@tanstack/react-query';

import { getQueueState, saveQueueState } from '@shared/api-client/playback';
import type { TrackResponse } from '@shared/api-client/types';
import { useQueueStore } from '@shared/playback/queueStore';
import { toPlaybackTrack } from '@shared/playback/toPlaybackTrack';
import type { RepeatMode } from '@shared/playback/types';

import { usePlayback } from './usePlayback';

const SAVE_INTERVAL_MS = 15_000;


function buildSourceId(source: ReturnType<typeof useQueueStore.getState>['source']): string {
  if (!source) return '';
  if (source.kind === 'playlist') return `playlist:${source.playlistId}`;
  return source.kind;
}

export function useQueueResume() {
  const { positionMs } = usePlayback();
  const queryClient = useQueryClient();
  const saveTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const restoredRef = useRef(false);

  const save = useCallback(() => {
    const s = useQueueStore.getState();
    if (s.tracks.length === 0) return;

    const trackIds = s.tracks
      .map((t) => (t.source.kind === 'library' ? t.source.trackId : ''))
      .filter(Boolean);

    void saveQueueState({
      track_ids: trackIds,
      current_index: s.currentIndex,
      position_ms: positionMs,
      shuffled: s.shuffled,
      repeat_mode: s.repeatMode,
      source_id: buildSourceId(s.source),
    });
  }, [positionMs]);

  useEffect(() => {
    if (restoredRef.current) return;
    restoredRef.current = true;

    void (async () => {
      try {
        const saved = await getQueueState();
        if (!saved.track_ids.length) return;

        const homeData = queryClient.getQueryData<{ items: TrackResponse[] }>(['library-home']);
        if (!homeData?.items.length) return;

        const trackMap = new Map(homeData.items.map((t) => [t.id, t]));
        const validTracks = saved.track_ids
          .map((id) => trackMap.get(id))
          .filter((t): t is TrackResponse => t != null && t.acquisition_status === 'ready');

        if (!validTracks.length) return;

        const playbackTracks = validTracks.map(toPlaybackTrack);
        const startIdx = Math.min(saved.current_index, playbackTracks.length - 1);

        useQueueStore.getState().loadQueue(playbackTracks, startIdx, null);

        const repeatMode = saved.repeat_mode as RepeatMode;
        if (repeatMode === 'all' || repeatMode === 'one') {
          useQueueStore.getState().cycleRepeatMode();
          if (repeatMode === 'one') useQueueStore.getState().cycleRepeatMode();
        }
        if (saved.shuffled) useQueueStore.getState().toggleShuffle();
      } catch {
        // Resume is best-effort
      }
    })();
  }, [queryClient]);

  useEffect(() => {
    saveTimerRef.current = setInterval(save, SAVE_INTERVAL_MS);
    return () => {
      if (saveTimerRef.current) clearInterval(saveTimerRef.current);
    };
  }, [save]);

  useEffect(() => {
    const sub = AppState.addEventListener('change', (state) => {
      if (state === 'background' || state === 'inactive') save();
    });
    return () => sub.remove();
  }, [save]);
}
