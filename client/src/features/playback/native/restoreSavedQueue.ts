import type { QueueStateResponse } from '@shared/api-client/playback';
import type { TrackResponse } from '@shared/api-client/trackResponse';
import { canPlay } from '@shared/playback/canPlay';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';

import { loadNativeQueue } from '@features/playback/native/loadNativeTrack';
import { playbackPort } from '@features/playback/ports/activePlaybackPort';
import { reportLoadFailure } from '@features/playback/hooks/usePlaybackErrorStore';
import {
  rebuildOnFirstWorkingRungReportingCurrent,
  showSavedTrackWhileRehydrating,
  type RungOutcome,
} from '@features/playback/domain/queueResume';
import {
  asRepeatMode,
  fromWireSource,
  parseQueueState,
} from '@features/playback/domain/queueStateWire';
import {
  recordQueueRestoreFailure,
  recordQueueStateRejected,
  type QueueRestoreStage,
} from '@features/playback/domain/playbackHealth';
import { warnPlayback } from '@features/playback/domain/playbackError';

function userTookOver(owned: number): boolean {
  return useQueueStore.getState().generation !== owned;
}

function savedTracksMissingFrom(
  saved: QueueStateResponse,
  home: readonly TrackResponse[],
): boolean {
  const present = new Set<string>(home.map((t) => t.id));
  return [...saved.natural_order, ...saved.track_ids].some((id) => !present.has(id));
}

function warnOnSavedTracksMissingFromLibrary(
  saved: QueueStateResponse,
  trackMap: ReadonlyMap<string, TrackResponse>,
): void {
  const savedIds = new Set([...saved.natural_order, ...saved.track_ids]);
  const missing = [...savedIds].filter((id) => !trackMap.has(id));
  if (!missing.length) return;
  console.warn('[playback] saved queue tracks missing from the library read; restoring without', {
    missing: missing.length,
    saved: savedIds.size,
  });
}

function rebuildSavedQueue(saved: QueueStateResponse, home: readonly TrackResponse[]): RungOutcome {
  const trackMap = new Map<string, TrackResponse>(home.map((t) => [t.id, t]));
  const isReady = (id: string): boolean => canPlay(trackMap.get(id)?.acquisition_status);
  const source = fromWireSource(saved.source);
  warnOnSavedTracksMissingFromLibrary(saved, trackMap);

  return rebuildOnFirstWorkingRungReportingCurrent(saved, trackMap, isReady, source);
}

function applyRepeatMode(wireRepeatMode: string): void {
  const repeatMode = asRepeatMode(wireRepeatMode);
  if (repeatMode === 'all' || repeatMode === 'one') {
    useQueueStore.getState().setRepeatMode(repeatMode);
  }
}

async function resumeNativeQueue(positionMs: number): Promise<void> {
  const owned = useQueueStore.getState().generation;
  const s = useQueueStore.getState();
  if (!s.currentTrack() || userTookOver(owned)) return;

  await loadNativeQueue(orderedQueueTracks(s), s.currentIndex, {
    autoplay: false,
    startPositionMs: positionMs,
  });
}

function clearUnbackedPlaceholder(
  placeholderGeneration: number | null,
  stage: QueueRestoreStage,
): void {
  if (placeholderGeneration == null) return;
  if (useQueueStore.getState().generation !== placeholderGeneration) return;
  useQueueStore.getState().clearQueue();
  console.warn('[playback] cleared the unbacked resume placeholder', { stage });
}

function reportUnbackedRebuild(rebuiltGeneration: number | null, err: unknown): void {
  const queue = useQueueStore.getState();
  if (rebuiltGeneration == null || queue.generation !== rebuiltGeneration) return;
  const current = queue.currentTrack();
  if (current) reportLoadFailure(current, err);
}

export async function restoreSavedQueue(
  markPlaceholderGeneration: (generation: number) => void,
): Promise<void> {
  let stage: QueueRestoreStage = 'fetch';
  let placeholderGeneration: number | null = null;
  let rebuiltGeneration: number | null = null;
  try {
    let owned = useQueueStore.getState().generation;

    const parsed = parseQueueState(await playbackPort().getQueueState());
    if (!parsed.ok) {
      console.warn(`[playback] rejected a malformed saved queue state: ${parsed.error.message}`);
      recordQueueStateRejected();
      return;
    }
    const saved = parsed.state;
    if (!saved.track_ids.length) return;
    if (userTookOver(owned)) return;

    stage = 'placeholder';
    placeholderGeneration = showSavedTrackWhileRehydrating(saved);
    if (placeholderGeneration != null) {
      owned = placeholderGeneration;
      markPlaceholderGeneration(placeholderGeneration);
    }

    stage = 'tracks';
    const { items: home, truncated } = await playbackPort().getAllTracks({});
    if (!home.length) return;
    if (userTookOver(owned)) return;
    if (truncated && savedTracksMissingFrom(saved, home)) {
      console.warn('[playback] library read hit its cap and lacks saved tracks; not restoring');
      return;
    }

    stage = 'rebuild';
    const rebuilt = rebuildSavedQueue(saved, home);
    if (rebuilt.rung === 'exhausted') return;
    const resumeMs = rebuilt.currentFound ? saved.position_ms : 0;
    useQueueStore.getState().setResumePosition(resumeMs);
    applyRepeatMode(saved.repeat_mode);
    rebuiltGeneration = useQueueStore.getState().generation;

    stage = 'native';
    await resumeNativeQueue(resumeMs);
  } catch (err) {
    warnPlayback('failed to restore the saved queue', { stage }, err);
    recordQueueRestoreFailure(stage);
    if (stage === 'native') reportUnbackedRebuild(rebuiltGeneration, err);
  } finally {
    clearUnbackedPlaceholder(placeholderGeneration, stage);
  }
}
