import { useCallback, useEffect, useRef } from 'react';
import { AppState } from 'react-native';

import TrackPlayer from 'react-native-track-player';

import { getQueueState, saveQueueState } from '@shared/api-client/playback';
import { getTracks } from '@shared/api-client/tracks';
import type { TrackResponse } from '@shared/api-client/types';
import { useQueueStore } from '@shared/playback/queueStore';
import { toPlaybackTrack } from '@shared/playback/toPlaybackTrack';
import type { RepeatMode } from '@shared/playback/types';

const SAVE_INTERVAL_MS = 15_000;
// Mirror of the library-home page size — resume rehydrates from the same
// /v1/tracks surface. Kept here so playback owns its own fetch instead of
// depending on the library screen having warmed its cache first.
const REHYDRATE_LIMIT = 2000;

function buildSourceId(source: ReturnType<typeof useQueueStore.getState>['source']): string {
  if (!source) return '';
  if (source.kind === 'playlist') return `playlist:${source.playlistId}`;
  return source.kind;
}

function parseSourceId(sourceId: string): ReturnType<typeof useQueueStore.getState>['source'] {
  if (!sourceId) return null;
  if (sourceId.startsWith('playlist:')) {
    return { kind: 'playlist', playlistId: sourceId.slice('playlist:'.length), name: '' };
  }
  if (sourceId === 'library') return { kind: 'library' };
  if (sourceId.startsWith('search')) return { kind: 'search', query: '' };
  return null;
}

export function useQueueResume() {
  const saveTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const restoredRef = useRef(false);

  const save = useCallback(async () => {
    const s = useQueueStore.getState();
    if (s.tracks.length === 0) return;

    const trackIds = s.tracks
      .map((t) => (t.source.kind === 'library' ? t.source.trackId : ''))
      .filter(Boolean);

    let posMs = 0;
    try {
      const progress = await TrackPlayer.getProgress();
      posMs = Math.round(progress.position * 1000);
    } catch {
      // Player may not be initialized yet
    }

    try {
      await saveQueueState({
        track_ids: trackIds,
        current_index: s.currentIndex,
        position_ms: posMs,
        shuffled: s.shuffled,
        repeat_mode: s.repeatMode,
        source_id: buildSourceId(s.source),
      });
    } catch {
      // Best-effort persistence — don't spam errors
    }
  }, []);

  useEffect(() => {
    if (restoredRef.current) return;
    restoredRef.current = true;

    void (async () => {
      try {
        const saved = await getQueueState();
        if (!saved.track_ids.length) return;

        // Rehydrate full track data through the shared api-client transport
        // rather than reaching into the library feature's React Query cache.
        // Resume no longer silently no-ops when the library screen hasn't loaded.
        const home = await getTracks({ limit: REHYDRATE_LIMIT, offset: 0 });
        if (!home.items.length) return;

        const trackMap = new Map(home.items.map((t) => [t.id, t]));
        const validTracks = saved.track_ids
          .map((id) => trackMap.get(id))
          .filter((t): t is TrackResponse => t != null && t.acquisition_status === 'ready');

        if (!validTracks.length) return;

        const playbackTracks = validTracks.map(toPlaybackTrack);
        const startIdx = Math.min(saved.current_index, playbackTracks.length - 1);
        const source = parseSourceId(saved.source_id);

        useQueueStore.getState().loadQueue(playbackTracks, startIdx, source);

        const repeatMode = saved.repeat_mode as RepeatMode;
        if (repeatMode === 'all' || repeatMode === 'one') {
          useQueueStore.getState().setRepeatMode(repeatMode);
        }
        if (saved.shuffled) useQueueStore.getState().toggleShuffle();
      } catch {
        // Resume is best-effort
      }
    })();
  }, []);

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
