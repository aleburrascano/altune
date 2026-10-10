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

type MarkPlaceholder = (generation: number) => void;
type Home = readonly TrackResponse[];

interface RestoreRun {
  stage: QueueRestoreStage;
  owned: number;
  placeholderGeneration: number | null;
  rebuiltGeneration: number | null;
}

function startRun(): RestoreRun {
  const owned = useQueueStore.getState().generation;
  return { stage: 'fetch', owned, placeholderGeneration: null, rebuiltGeneration: null };
}

async function readSavedQueue(owned: number): Promise<QueueStateResponse | null> {
  const parsed = parseQueueState(await playbackPort().getQueueState());
  if (!parsed.ok) {
    console.warn(`[playback] rejected a malformed saved queue state: ${parsed.error.message}`);
    recordQueueStateRejected();
    return null;
  }
  if (!parsed.state.track_ids.length || userTookOver(owned)) return null;
  return parsed.state;
}

function showPlaceholder(saved: QueueStateResponse, run: RestoreRun, mark: MarkPlaceholder): void {
  run.stage = 'placeholder';
  const generation = showSavedTrackWhileRehydrating(saved);
  run.placeholderGeneration = generation;
  if (generation == null) return;
  run.owned = generation;
  mark(generation);
}

function capLosesSavedTracks(saved: QueueStateResponse, home: Home, truncated: boolean): boolean {
  if (!truncated || !savedTracksMissingFrom(saved, home)) return false;
  console.warn('[playback] library read hit its cap and lacks saved tracks; not restoring');
  return true;
}

async function readLibrary(saved: QueueStateResponse, run: RestoreRun): Promise<Home | null> {
  run.stage = 'tracks';
  const { items, truncated } = await playbackPort().getAllTracks({});
  if (!items.length || userTookOver(run.owned)) return null;
  return capLosesSavedTracks(saved, items, truncated) ? null : items;
}

function applyRebuilt(saved: QueueStateResponse, resumeMs: number): void {
  useQueueStore.getState().setResumePosition(resumeMs);
  applyRepeatMode(saved.repeat_mode);
}

function rebuildAndApply(saved: QueueStateResponse, home: Home, run: RestoreRun): number | null {
  run.stage = 'rebuild';
  const rebuilt = rebuildSavedQueue(saved, home);
  if (rebuilt.rung === 'exhausted') return null;
  const resumeMs = rebuilt.currentFound ? saved.position_ms : 0;
  applyRebuilt(saved, resumeMs);
  run.rebuiltGeneration = useQueueStore.getState().generation;
  return resumeMs;
}

async function rebuildAndResume(
  saved: QueueStateResponse,
  home: Home,
  run: RestoreRun,
): Promise<void> {
  const resumeMs = rebuildAndApply(saved, home, run);
  if (resumeMs == null) return;
  run.stage = 'native';
  await resumeNativeQueue(resumeMs);
}

async function runRestore(mark: MarkPlaceholder, run: RestoreRun): Promise<void> {
  const saved = await readSavedQueue(run.owned);
  if (!saved) return;
  showPlaceholder(saved, run, mark);
  const home = await readLibrary(saved, run);
  if (home) await rebuildAndResume(saved, home, run);
}

function reportRestoreFailure(run: RestoreRun, err: unknown): void {
  warnPlayback('failed to restore the saved queue', { stage: run.stage }, err);
  recordQueueRestoreFailure(run.stage);
  if (run.stage === 'native') reportUnbackedRebuild(run.rebuiltGeneration, err);
}

export async function restoreSavedQueue(markPlaceholder: MarkPlaceholder): Promise<void> {
  const run = startRun();
  try {
    await runRestore(markPlaceholder, run);
  } catch (err) {
    reportRestoreFailure(run, err);
  } finally {
    clearUnbackedPlaceholder(run.placeholderGeneration, run.stage);
  }
}
