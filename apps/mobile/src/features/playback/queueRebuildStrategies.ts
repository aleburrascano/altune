import type { QueueStateResponse } from '@shared/api-client/playback';
import type { TrackResponse } from '@shared/api-client/types';
import { useQueueStore } from '@shared/playback/queueStore';
import { currentTrackToPlaybackTrack, toPlaybackTrack } from '@shared/playback/toPlaybackTrack';
import type { QueueSource } from '@shared/playback/types';

import { fromWireSource } from './queueStateWire';
import { currentTrackId, reconstructPlayOrder, resolveResumeStartIndex } from './resumeQueue';

export function showSavedTrackWhileRehydrating(saved: QueueStateResponse): number | null {
  if (!saved.current_track || saved.current_track.acquisition_status !== 'ready') return null;

  const current = currentTrackToPlaybackTrack(saved.current_track);
  useQueueStore.getState().loadQueue([current], 0, fromWireSource(saved.source));
  useQueueStore.getState().setResumePosition(saved.position_ms);
  return useQueueStore.getState().generation;
}

export function rebuildFromNaturalOrder(
  saved: QueueStateResponse,
  trackMap: Map<string, TrackResponse>,
  isReady: (id: string) => boolean,
  source: QueueSource | null,
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

export function rebuildFromPlayOrderAlone(
  saved: QueueStateResponse,
  trackMap: Map<string, TrackResponse>,
  source: QueueSource | null,
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
