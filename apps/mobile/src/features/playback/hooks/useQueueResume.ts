import { useCallback, useEffect, useRef } from 'react';
import type { AppStateStatus } from 'react-native';

import TrackPlayer from 'react-native-track-player';

import { getQueueState, saveQueueState } from '@shared/api-client/playback';
import { getTracks } from '@shared/api-client/tracks';
import type { TrackResponse } from '@shared/api-client/types';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';

import { loadNativeQueue } from '../loadNativeTrack';
import {
  rebuildFromNaturalOrder,
  rebuildFromPlayOrderAlone,
  showSavedTrackWhileRehydrating,
} from '../queueRebuildStrategies';
import { asRepeatMode, fromWireSource, toWireSource } from '../queueStateWire';

import { useAppStateChange } from './useAppStateChange';

const SAVE_INTERVAL_MS = 15_000;
const REHYDRATE_LIMIT = 2000;

async function currentPositionMsOrZero(): Promise<number> {
  try {
    const progress = await TrackPlayer.getProgress();
    return Math.round(progress.position * 1000);
  } catch {
    return 0;
  }
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
        source: toWireSource(s.source),
        natural_order: naturalOrder,
      });
    } catch {
      console.warn('[playback] failed to save queue state');
    }
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

        const trackMap = new Map<string, TrackResponse>(home.items.map((t) => [t.id, t]));
        const isReady = (id: string): boolean => {
          const t = trackMap.get(id);
          return t != null && t.acquisition_status === 'ready';
        };
        const source = fromWireSource(saved.source);

        const rebuilt =
          rebuildFromNaturalOrder(saved, trackMap, isReady, source) ||
          rebuildFromPlayOrderAlone(saved, trackMap, source);
        if (!rebuilt) return;

        useQueueStore.getState().setResumePosition(saved.position_ms);

        const repeatMode = asRepeatMode(saved.repeat_mode);
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
      } catch {
        console.warn('[playback] failed to restore the saved queue');
      }
    })();
  }, []);

  useEffect(() => {
    saveTimerRef.current = setInterval(() => {
      void save();
    }, SAVE_INTERVAL_MS);
    return () => {
      if (saveTimerRef.current) clearInterval(saveTimerRef.current);
    };
  }, [save]);

  const saveOnLeave = useCallback(
    (state: AppStateStatus): void => {
      if (state === 'background' || state === 'inactive') void save();
    },
    [save],
  );
  useAppStateChange(saveOnLeave);
}
