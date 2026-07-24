import { useCallback, useEffect, useRef } from 'react';
import { AppState } from 'react-native';

import TrackPlayer from 'react-native-track-player';

import { getQueueState, saveQueueState } from '@shared/api-client/playback';
import { getTracks } from '@shared/api-client/tracks';
import type { TrackResponse } from '@shared/api-client/types';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import { currentTrackToPlaybackTrack, toPlaybackTrack } from '@shared/playback/toPlaybackTrack';
import type { RepeatMode } from '@shared/playback/types';

import { loadNativeQueue } from '../loadNativeTrack';
import { currentTrackId, reconstructPlayOrder, resolveResumeStartIndex } from '../resumeQueue';

const SAVE_INTERVAL_MS = 15_000;
const REHYDRATE_LIMIT = 2000;

function buildSourceId(source: ReturnType<typeof useQueueStore.getState>['source']): string {
  if (!source) return '';
  if (source.kind === 'playlist') {
    return `playlist:${source.playlistId}:${encodeURIComponent(source.name)}`;
  }
  if (source.kind === 'search') return `search:${encodeURIComponent(source.query)}`;
  return source.kind;
}

function decodeOrEmpty(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return '';
  }
}

function parseSourceId(sourceId: string): ReturnType<typeof useQueueStore.getState>['source'] {
  if (!sourceId) return null;
  if (sourceId.startsWith('playlist:')) {
    const rest = sourceId.slice('playlist:'.length);
    const sep = rest.indexOf(':');
    if (sep === -1) return { kind: 'playlist', playlistId: rest, name: '' };
    return {
      kind: 'playlist',
      playlistId: rest.slice(0, sep),
      name: decodeOrEmpty(rest.slice(sep + 1)),
    };
  }
  if (sourceId === 'library') return { kind: 'library' };
  if (sourceId === 'search') return { kind: 'search', query: '' };
  if (sourceId.startsWith('search:')) {
    return { kind: 'search', query: decodeOrEmpty(sourceId.slice('search:'.length)) };
  }
  return null;
}

async function currentPositionMsOrZero(): Promise<number> {
  try {
    const progress = await TrackPlayer.getProgress();
    return Math.round(progress.position * 1000);
  } catch {
    return 0;
  }
}

function showSavedTrackWhileRehydrating(
  saved: Awaited<ReturnType<typeof getQueueState>>,
): number | null {
  if (!saved.current_track || saved.current_track.acquisition_status !== 'ready') return null;

  const current = currentTrackToPlaybackTrack(saved.current_track);
  useQueueStore.getState().loadQueue([current], 0, parseSourceId(saved.source_id));
  useQueueStore.getState().setResumePosition(saved.position_ms);
  return useQueueStore.getState().generation;
}

function rebuildFromNaturalOrder(
  saved: Awaited<ReturnType<typeof getQueueState>>,
  trackMap: Map<string, TrackResponse>,
  isReady: (id: string) => boolean,
  source: ReturnType<typeof parseSourceId>,
): boolean {
  if (!saved.natural_order.length) return false;

  const naturalIds = saved.natural_order.filter(isReady);
  const playIds = saved.track_ids.filter(isReady);
  const currentId = currentTrackId(saved.track_ids, saved.current_index);
  const { playOrder, currentIndex } = reconstructPlayOrder(naturalIds, playIds, currentId);
  if (!naturalIds.length || !playOrder.length) return false;

  const naturalTracks = naturalIds.map((id) => toPlaybackTrack(trackMap.get(id)!));
  useQueueStore
    .getState()
    .restoreQueue(naturalTracks, playOrder, currentIndex, source, saved.shuffled);
  return true;
}

function rebuildFromPlayOrderAlone(
  saved: Awaited<ReturnType<typeof getQueueState>>,
  trackMap: Map<string, TrackResponse>,
  source: ReturnType<typeof parseSourceId>,
): boolean {
  const validTracks = saved.track_ids
    .map((id) => trackMap.get(id))
    .filter((t): t is TrackResponse => t != null && t.acquisition_status === 'ready');
  if (!validTracks.length) return false;

  const startIdx = resolveResumeStartIndex(
    saved.track_ids,
    saved.current_index,
    validTracks.map((t) => t.id),
  );
  useQueueStore.getState().loadQueue(validTracks.map(toPlaybackTrack), startIdx, source);
  if (saved.shuffled) useQueueStore.getState().setShuffled(true);
  return true;
}

export function useQueueResume() {
  const saveTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const restoredRef = useRef(false);
  const placeholderGenerationRef = useRef<number | null>(null);

  const save = useCallback(async () => {
    const s = useQueueStore.getState();
    if (s.tracks.length === 0) return;
    if (placeholderGenerationRef.current === s.generation) return;

    const trackIds = orderedQueueTracks(s)
      .map((t) => (t.source.kind === 'library' ? t.source.trackId : ''))
      .filter(Boolean);
    const naturalOrder = s.tracks
      .map((t) => (t.source.kind === 'library' ? t.source.trackId : ''))
      .filter(Boolean);
    const current = s.currentTrack();
    const currentId = current && current.source.kind === 'library' ? current.source.trackId : '';
    const currentIndex = currentId ? Math.max(0, trackIds.indexOf(currentId)) : 0;

    try {
      await saveQueueState({
        track_ids: trackIds,
        current_index: currentIndex,
        position_ms: await currentPositionMsOrZero(),
        shuffled: s.shuffled,
        repeat_mode: s.repeatMode,
        source_id: buildSourceId(s.source),
        natural_order: naturalOrder,
      });
    } catch {}
  }, []);

  useEffect(() => {
    if (restoredRef.current) return;
    restoredRef.current = true;

    void (async () => {
      const userTookOver = (owned: number): boolean =>
        useQueueStore.getState().generation !== owned;

      try {
        let owned = useQueueStore.getState().generation;
        const saved = await getQueueState();
        if (!saved.track_ids.length) return;
        if (userTookOver(owned)) return;

        const placeholderGeneration = showSavedTrackWhileRehydrating(saved);
        if (placeholderGeneration != null) {
          owned = placeholderGeneration;
          placeholderGenerationRef.current = placeholderGeneration;
        }

        const home = await getTracks({ limit: REHYDRATE_LIMIT, offset: 0 });
        if (!home.items.length) return;
        if (userTookOver(owned)) return;

        const trackMap = new Map(home.items.map((t) => [t.id, t]));
        const isReady = (id: string): boolean => {
          const t = trackMap.get(id);
          return t != null && t.acquisition_status === 'ready';
        };
        const source = parseSourceId(saved.source_id);

        const rebuilt =
          rebuildFromNaturalOrder(saved, trackMap, isReady, source) ||
          rebuildFromPlayOrderAlone(saved, trackMap, source);
        if (!rebuilt) return;

        useQueueStore.getState().setResumePosition(saved.position_ms);

        const repeatMode = saved.repeat_mode as RepeatMode;
        if (repeatMode === 'all' || repeatMode === 'one') {
          useQueueStore.getState().setRepeatMode(repeatMode);
        }

        owned = useQueueStore.getState().generation;
        const s = useQueueStore.getState();
        if (s.currentTrack() && !userTookOver(owned)) {
          await loadNativeQueue(orderedQueueTracks(s), s.currentIndex, {
            autoplay: false,
            startPositionMs: saved.position_ms,
          });
        }
      } catch {}
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
