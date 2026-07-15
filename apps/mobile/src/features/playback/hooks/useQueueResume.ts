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
// Mirror of the library-home page size — resume rehydrates from the same
// /v1/tracks surface. Kept here so playback owns its own fetch instead of
// depending on the library screen having warmed its cache first.
const REHYDRATE_LIMIT = 2000;

// source_id wire format:
//   library
//   playlist:<playlistId>[:<url-encoded name>]
//   search[:<url-encoded query>]
// The trailing name/query are encoded so a ':' or space in either can't be
// mistaken for a separator. Both are optional: rows written before they were
// persisted parse to '' rather than failing.
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
    // Dropping the name here rendered the queue sheet's "Playing from <name>"
    // with an empty name after every resume from a playlist.
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

export function useQueueResume() {
  const saveTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const restoredRef = useRef(false);
  // Generation of the deliberate one-track placeholder restore loads before it
  // rehydrates the full queue. Persisting THAT would overwrite the real saved
  // queue with a single track — and a slow or failed rehydrate (bad network on
  // launch) would make the truncation permanent. Pinning the exact generation
  // rather than a "restoring" flag keeps the gate narrow: the moment the queue
  // becomes anything else — rehydrated, or replaced by a user tap — it is real
  // again and saving resumes, even if the rehydrate never finished.
  const placeholderGenerationRef = useRef<number | null>(null);

  const save = useCallback(async () => {
    const s = useQueueStore.getState();
    if (s.tracks.length === 0) return;
    if (placeholderGenerationRef.current === s.generation) return;

    // Persist in PLAY ORDER with current_index pointing into that same library-only
    // list, so track_ids[current_index] is unambiguously the current track. The old
    // format sent track_ids in natural order but current_index as a play-order
    // position, so they disagreed whenever the queue was shuffled or reordered —
    // restoring (and the server-embedded now-playing snapshot) landed on the wrong
    // track.
    const trackIds = orderedQueueTracks(s)
      .map((t) => (t.source.kind === 'library' ? t.source.trackId : ''))
      .filter(Boolean);
    // natural_order is the same library tracks in their pre-shuffle (album/playlist)
    // order — s.tracks is natural order; track_ids above is play order. Persisting
    // both lets restore rebuild the exact shuffled sequence AND un-shuffle back to
    // the original order after relaunch.
    const naturalOrder = s.tracks
      .map((t) => (t.source.kind === 'library' ? t.source.trackId : ''))
      .filter(Boolean);
    const current = s.currentTrack();
    const currentId =
      current && current.source.kind === 'library' ? current.source.trackId : '';
    const currentIndex = currentId ? Math.max(0, trackIds.indexOf(currentId)) : 0;

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
        current_index: currentIndex,
        position_ms: posMs,
        shuffled: s.shuffled,
        repeat_mode: s.repeatMode,
        source_id: buildSourceId(s.source),
        natural_order: naturalOrder,
      });
    } catch {
      // Best-effort persistence — don't spam errors
    }
  }, []);

  useEffect(() => {
    if (restoredRef.current) return;
    restoredRef.current = true;

    void (async () => {
      // Resume owns the queue only until the user picks something. Every await
      // below is a window in which a tap can start real playback; `owned` is the
      // generation resume last wrote, so a mismatch means the user took over and
      // restoring would silently stop their music and swap in yesterday's queue.
      const userTookOver = (owned: number): boolean =>
        useQueueStore.getState().generation !== owned;

      try {
        let owned = useQueueStore.getState().generation;
        const saved = await getQueueState();
        if (!saved.track_ids.length) return;
        if (userTookOver(owned)) return;

        // Instant now-playing: the server embeds the current track's metadata, so
        // render it (and the saved scrubber position) from this small call before
        // the slow full-library rehydrate below. A one-track placeholder queue is
        // display-only — the native player is primed later with the full queue.
        // Same trackId identity as the rehydrated entry, so the swap is seamless.
        if (saved.current_track && saved.current_track.acquisition_status === 'ready') {
          const current = currentTrackToPlaybackTrack(saved.current_track);
          useQueueStore.getState().loadQueue([current], 0, parseSourceId(saved.source_id));
          useQueueStore.getState().setResumePosition(saved.position_ms);
          owned = useQueueStore.getState().generation;
          placeholderGenerationRef.current = owned;
        }

        // Rehydrate full track data through the shared api-client transport
        // rather than reaching into the library feature's React Query cache.
        // Resume no longer silently no-ops when the library screen hasn't loaded.
        const home = await getTracks({ limit: REHYDRATE_LIMIT, offset: 0 });
        if (!home.items.length) return;
        // The widest window — this fetch is the whole library by design.
        if (userTookOver(owned)) return;

        const trackMap = new Map(home.items.map((t) => [t.id, t]));
        const isReady = (id: string): boolean => {
          const t = trackMap.get(id);
          return t != null && t.acquisition_status === 'ready';
        };
        const source = parseSourceId(saved.source_id);

        // Path 1 — full fidelity: with the persisted natural (unshuffled) order we
        // rebuild the store with an explicit play-order permutation, so the exact
        // shuffled sequence resumes AND un-shuffle returns to the album/playlist
        // order. tracks stays in natural order; playOrder carries the shuffle.
        let loaded = false;
        if (saved.natural_order.length) {
          const naturalIds = saved.natural_order.filter(isReady);
          const playIds = saved.track_ids.filter(isReady);
          const currentId = currentTrackId(saved.track_ids, saved.current_index);
          const { playOrder, currentIndex } = reconstructPlayOrder(naturalIds, playIds, currentId);
          if (naturalIds.length && playOrder.length) {
            const naturalTracks = naturalIds.map((id) => toPlaybackTrack(trackMap.get(id)!));
            useQueueStore
              .getState()
              .restoreQueue(naturalTracks, playOrder, currentIndex, source, saved.shuffled);
            loaded = true;
          }
        }

        // Path 2 — fallback for older rows without natural_order: treat track_ids as
        // the queue and locate the current track by id (robust to filter shifts).
        if (!loaded) {
          const validTracks = saved.track_ids
            .map((id) => trackMap.get(id))
            .filter((t): t is TrackResponse => t != null && t.acquisition_status === 'ready');
          if (!validTracks.length) return;

          const startIdx = resolveResumeStartIndex(
            saved.track_ids,
            saved.current_index,
            validTracks.map((t) => t.id),
          );
          useQueueStore.getState().loadQueue(validTracks.map(toPlaybackTrack), startIdx, source);
          if (saved.shuffled) useQueueStore.getState().setShuffled(true);
        }

        // Both paths cleared resumePositionMs; re-seed so the scrubber keeps showing
        // the saved offset until the native player seeks and reports live progress.
        useQueueStore.getState().setResumePosition(saved.position_ms);

        const repeatMode = saved.repeat_mode as RepeatMode;
        if (repeatMode === 'all' || repeatMode === 'one') {
          useQueueStore.getState().setRepeatMode(repeatMode);
        }

        // Prime the native player with the full restored queue, paused and
        // seeked to the saved position. loadQueue only updates the store;
        // without this the native queue stays empty and pressing play after a
        // relaunch does nothing. Read state AFTER shuffle/repeat so the native
        // order matches the pinned-current play order. autoplay:false leaves it
        // paused so the app never blares audio on its own — the user taps play.
        // Re-read: the store writes above bumped the generation, and a tap during
        // them means the user's queue is live and must not be primed over.
        owned = useQueueStore.getState().generation;
        const s = useQueueStore.getState();
        if (s.currentTrack() && !userTookOver(owned)) {
          await loadNativeQueue(orderedQueueTracks(s), s.currentIndex, {
            autoplay: false,
            startPositionMs: saved.position_ms,
          });
        }
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
