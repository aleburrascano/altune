import type { QueueStateResponse } from '@shared/api-client/playback';
import type { TrackResponse } from '@shared/api-client/types';
import { canPlay } from '@shared/playback/canPlay';
import { useQueueStore } from '@shared/playback/queueStore';
import { currentTrackToPlaybackTrack, toPlaybackTrack } from '@shared/playback/toPlaybackTrack';
import type { QueueSource } from '@shared/playback/types';

import { recordQueueRebuildOutcome, type QueueRebuildRung } from './playbackHealth';
import { fromWireSource } from './queueStateWire';
import {
  currentOccurrence,
  currentTrackId,
  reconstructPlayOrder,
  resolveResumeStartIndex,
} from './resumeQueue';

export function showSavedTrackWhileRehydrating(saved: QueueStateResponse): number | null {
  if (!saved.current_track || !canPlay(saved.current_track.acquisition_status)) return null;

  const current = currentTrackToPlaybackTrack(saved.current_track);
  useQueueStore.getState().loadQueue([current], 0, fromWireSource(saved.source));
  useQueueStore.getState().setResumePosition(saved.position_ms);
  return useQueueStore.getState().generation;
}

// The ladder degrades in place, and every rung leaves the user with something that looks like
// an ordinary resume, so a backend change that pushes most resumes down a rung is invisible
// without the tally (#1727). Each rung is recorded where the ladder is walked rather than
// inside the rungs, so one resume contributes exactly one outcome.
export function rebuildOnFirstWorkingRung(
  saved: QueueStateResponse,
  trackMap: Map<string, TrackResponse>,
  isReady: (id: string) => boolean,
  source: QueueSource | null,
): QueueRebuildRung {
  if (rebuildFromNaturalOrder(saved, trackMap, isReady, source)) return recordedRung('natural');
  if (rebuildFromPlayOrderAlone(saved, trackMap, source)) return recordedRung('play_order');
  return recordedRung('exhausted');
}

// Returns the rung it recorded, so no path down the ladder can return without being counted.
function recordedRung(rung: QueueRebuildRung): QueueRebuildRung {
  recordQueueRebuildOutcome(rung);
  return rung;
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
  const { playOrder, currentIndex } = reconstructPlayOrder(
    naturalIds,
    playIds,
    currentTrackId(saved.track_ids, saved.current_index),
    currentOccurrence(saved.track_ids, saved.current_index),
  );
  if (!naturalIds.length || !playOrder.length) return false;

  const naturalTracks = naturalIds.map((id) => toPlaybackTrack(trackMap.get(id)!));
  useQueueStore.getState().restoreQueue({
    tracks: naturalTracks,
    playOrder,
    currentIndex,
    source,
    shuffled: saved.shuffled,
  });
  return true;
}

export function rebuildFromPlayOrderAlone(
  saved: QueueStateResponse,
  trackMap: Map<string, TrackResponse>,
  source: QueueSource | null,
): boolean {
  const validTracks = saved.track_ids
    .map((id) => trackMap.get(id))
    .filter((t): t is TrackResponse => t != null && canPlay(t.acquisition_status));
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
