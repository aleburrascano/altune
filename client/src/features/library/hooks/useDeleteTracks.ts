import { useCallback, useEffect, useRef } from 'react';
import { useQueryClient, type QueryClient } from '@tanstack/react-query';
import { useAppMutation } from '@shared/query/useAppMutation';
import { showFailureAlert } from '@shared/ui';
import type { TrackId } from '@shared/api-client/ids';
import { forgetTrack } from '@shared/events/forgetTrack';
import { invalidateLibraryDerived } from '@shared/events/trackCachePatch';
import { TRACK_CACHE_FAMILIES } from '@shared/events/trackCacheShapes';
import { usePinnedStore } from '@shared/offline/pinnedStore';
import { libraryKeys } from '@shared/query/queryKeys';
import {
  currentSessionEpoch,
  guardedMutationOptions,
  isSameSession,
} from '@shared/session/signOutCleanup';
import { useLibraryPort } from '@features/library/ports/LibraryPortProvider';
import type { LibraryPort } from '@features/library/ports/libraryPort';
import {
  logTrackMutationFailure,
  failureLogFields,
  libraryFailureAlert,
  classifyLibraryError,
  failureTail,
  type LibraryFailure,
} from '@features/library/domain/libraryFailure';
import {
  captureTrackPlacements,
  restoreTrackPlacements,
  type TrackCachePlacement,
} from '@shared/events/trackPlacements';
import {
  patchTrackStatus,
  useTrackStatusStore,
  type TrackStatus,
} from '@shared/acquisition/trackStatusStore';

const deleteEndpoint = (trackId: TrackId) => `DELETE /v1/tracks/${trackId}`;

export const BULK_DELETE_CONCURRENCY = 4;
export const BULK_DELETE_DEADLINE_MS = 60_000;

type DeleteTrackFailure = { trackId: TrackId; error: unknown };

type DeleteTracksResult = {
  deleted: number;
  requested: number;
  failures: DeleteTrackFailure[];
  skipped: number;
  cancelled: boolean;
};

type OnDeleted = (trackId: TrackId) => void;
type DeleteAttempt = DeleteTrackFailure | undefined;
type BatchRun = { stopped: () => boolean; expired: () => boolean };
type Outcome = { sent: number; failures: DeleteTrackFailure[] };
type RunHandle = { stopped: () => boolean; inStartingSession: () => boolean; finish: () => void };
type Deadline = { expired: () => boolean; clear: () => void };
type Cursor = { taken: number };
type RemoveTrack = (trackId: TrackId) => Promise<void>;
type BatchContext = {
  cursor: Cursor;
  trackIds: TrackId[];
  run: BatchRun;
  onDeleted: OnDeleted;
  remove: RemoveTrack;
  failed: DeleteAttempt[];
};

export function deleteOne(port: LibraryPort, trackId: TrackId): Promise<void> {
  return port.deleteTrack(trackId);
}

async function deleteTrackForBulk(trackId: TrackId, ctx: BatchContext): Promise<DeleteAttempt> {
  try {
    await ctx.remove(trackId);
  } catch (error) {
    if (classifyLibraryError(error) !== 'not-found') return { trackId, error };
  }
  ctx.onDeleted(trackId);
  return undefined;
}

function takeNextIndex(cursor: Cursor, trackIds: TrackId[], run: BatchRun): number | undefined {
  if (cursor.taken >= trackIds.length || run.stopped() || run.expired()) return undefined;
  return cursor.taken++;
}

async function drainWorker(ctx: BatchContext): Promise<void> {
  let index = takeNextIndex(ctx.cursor, ctx.trackIds, ctx.run);
  while (index !== undefined) {
    const failure = await deleteTrackForBulk(ctx.trackIds[index]!, ctx);
    if (failure) ctx.failed[index] = failure;
    index = takeNextIndex(ctx.cursor, ctx.trackIds, ctx.run);
  }
}

function collectFailures(failed: DeleteAttempt[]): DeleteTrackFailure[] {
  return failed.filter((failure): failure is DeleteTrackFailure => failure != null);
}

function newContext(
  trackIds: TrackId[],
  run: BatchRun,
  onDeleted: OnDeleted,
  remove: RemoveTrack,
): BatchContext {
  return { cursor: { taken: 0 }, trackIds, run, onDeleted, remove, failed: [] };
}

async function deleteInBatches(ctx: BatchContext): Promise<Outcome> {
  const size = Math.min(BULK_DELETE_CONCURRENCY, ctx.trackIds.length);
  await Promise.all(Array.from({ length: size }, () => drainWorker(ctx)));
  return { sent: ctx.cursor.taken, failures: collectFailures(ctx.failed) };
}

function startDeadline(): Deadline {
  let expired = false;
  const timer = setTimeout(() => (expired = true), BULK_DELETE_DEADLINE_MS);
  return { expired: () => expired, clear: () => clearTimeout(timer) };
}

type CleanupState = { failed: boolean };

function guardCleanup(state: CleanupState, trackId: TrackId, step: () => void): void {
  try {
    step();
  } catch (error) {
    state.failed = true;
    console.warn('[library] delete cleanup failed', { trackId, ...failureLogFields(error) });
  }
}

function cleanupTrack(queryClient: QueryClient, state: CleanupState, trackId: TrackId): void {
  guardCleanup(state, trackId, () => forgetTrack(queryClient, trackId));
  guardCleanup(state, trackId, () => usePinnedStore.getState().unpin(trackId));
}

function removeTrackEverywhere(
  queryClient: QueryClient,
  handle: RunHandle,
  state: CleanupState,
): OnDeleted {
  return (trackId) => {
    if (!handle.inStartingSession()) return;
    cleanupTrack(queryClient, state, trackId);
  };
}

function resyncTrackCaches(queryClient: QueryClient): void {
  for (const { prefix } of Object.values(TRACK_CACHE_FAMILIES)) {
    void queryClient.invalidateQueries({ queryKey: prefix });
  }
}

function finalizeRun(deadline: Deadline, handle: RunHandle): () => void {
  return () => {
    deadline.clear();
    handle.finish();
  };
}

function summarizeRun(ids: TrackId[], outcome: Outcome, cancelled: boolean): DeleteTracksResult {
  return {
    deleted: outcome.sent - outcome.failures.length,
    requested: ids.length,
    failures: outcome.failures,
    skipped: ids.length - outcome.sent,
    cancelled,
  };
}

function openTimedRun(handle: RunHandle): { run: BatchRun; done: () => void } {
  const deadline = startDeadline();
  const stopped = () => handle.stopped() || !handle.inStartingSession();
  const run: BatchRun = { stopped, expired: deadline.expired };
  return { run, done: finalizeRun(deadline, handle) };
}

async function runBulkDelete(
  queryClient: QueryClient,
  handle: RunHandle,
  trackIds: TrackId[],
  remove: RemoveTrack,
): Promise<DeleteTracksResult> {
  const { run, done } = openTimedRun(handle);
  const cleanup: CleanupState = { failed: false };
  const ctx = newContext(
    trackIds,
    run,
    removeTrackEverywhere(queryClient, handle, cleanup),
    remove,
  );
  const outcome = await deleteInBatches(ctx).finally(done);
  if (cleanup.failed && handle.inStartingSession()) resyncTrackCaches(queryClient);
  return summarizeRun(trackIds, outcome, handle.stopped());
}

function createRunHandle(stops: Set<() => void>): RunHandle {
  let stopped = false;
  const epoch = currentSessionEpoch();
  const stop = () => (stopped = true);
  stops.add(stop);
  const inStartingSession = () => isSameSession(epoch);
  return { stopped: () => stopped, inStartingSession, finish: () => stops.delete(stop) };
}

function useUnmountStop(): () => RunHandle {
  const stops = useRef(new Set<() => void>());
  useEffect(() => {
    const active = stops.current;
    return () => active.forEach((stop) => stop());
  }, []);
  return useCallback(() => createRunHandle(stops.current), []);
}

function logBulkFailure({ trackId, error }: DeleteTrackFailure): void {
  logTrackMutationFailure('delete track', deleteEndpoint, trackId, error);
}

function dominantFailure(failures: DeleteTrackFailure[]): LibraryFailure {
  const kinds = failures.map(({ error }) => classifyLibraryError(error));
  return kinds.includes('auth') ? 'auth' : kinds[0]!;
}

function bulkFailureMessage(requested: number, deleted: number, failures: DeleteTrackFailure[]) {
  const tail = failureTail(dominantFailure(failures));
  return `${requested - deleted} of ${requested} tracks could not be removed. ${tail}`;
}

function reportBulkOutcome(queryClient: QueryClient, summary: DeleteTracksResult): void {
  const { deleted, requested, failures, cancelled } = summary;
  if (deleted > 0) invalidateLibraryDerived(queryClient);
  failures.forEach(logBulkFailure);
  if (cancelled || deleted === requested) return;
  showFailureAlert({
    surface: 'library.delete_tracks',
    title: 'Delete failed',
    message: bulkFailureMessage(requested, deleted, failures),
  });
}

const LOGGED_TRACK_IDS = 20;

function logBulkRunFailure(error: unknown, trackIds: TrackId[]): void {
  console.warn('[library] bulk delete failed', {
    requested: trackIds.length,
    trackIds: trackIds.slice(0, LOGGED_TRACK_IDS),
    ...failureLogFields(error),
  });
}

function recoverFailedBulkRun(queryClient: QueryClient) {
  return (error: Error, trackIds: TrackId[]): void => {
    invalidateLibraryDerived(queryClient);
    void queryClient.invalidateQueries({ queryKey: libraryKeys.tracksPrefix });
    logBulkRunFailure(error, trackIds);
    showFailureAlert(
      libraryFailureAlert(
        'library.delete_tracks',
        'Delete failed',
        'Could not remove these tracks.',
        classifyLibraryError(error),
      ),
    );
  };
}

export function useDeleteTracks() {
  const queryClient = useQueryClient();
  const startRun = useUnmountStop();
  const port = useLibraryPort();
  return useAppMutation({
    ...guardedMutationOptions({
      mutationFn: (trackIds: TrackId[]) =>
        runBulkDelete(queryClient, startRun(), trackIds, (id) => deleteOne(port, id)),
      onSuccess: (summary: DeleteTracksResult) => reportBulkOutcome(queryClient, summary),
      onError: recoverFailedBulkRun(queryClient),
    }),
    action: 'library.delete_tracks',
  });
}

type RemovedTrack = { placements: TrackCachePlacement[]; status: TrackStatus | undefined };

function unpin(trackId: TrackId): void {
  usePinnedStore.getState().unpin(trackId);
}

function removeOptimistically(queryClient: QueryClient) {
  return (trackId: TrackId): RemovedTrack => {
    const placements = captureTrackPlacements(queryClient, trackId);
    const status = useTrackStatusStore.getState().statuses[trackId];
    forgetTrack(queryClient, trackId);
    return { placements, status };
  };
}

function confirmRemoval(queryClient: QueryClient) {
  return (_deleted: void, trackId: TrackId): void => {
    unpin(trackId);
    invalidateLibraryDerived(queryClient);
  };
}

function putTrackBack(queryClient: QueryClient, trackId: TrackId, removed: RemovedTrack): void {
  restoreTrackPlacements(queryClient, removed.placements);
  if (removed.status) patchTrackStatus(trackId, removed.status);
}

function undoFailedRemoval(queryClient: QueryClient) {
  return (error: Error, trackId: TrackId, removed: RemovedTrack): void => {
    const failure = classifyLibraryError(error);
    if (failure === 'not-found') return unpin(trackId);
    logTrackMutationFailure('delete track', deleteEndpoint, trackId, error);
    putTrackBack(queryClient, trackId, removed);
    showFailureAlert(
      libraryFailureAlert(
        'library.delete_track',
        'Delete failed',
        'Could not remove the track.',
        failure,
      ),
    );
  };
}

function deleteTrackOptions(queryClient: QueryClient, port: LibraryPort) {
  return guardedMutationOptions({
    mutationFn: (trackId: TrackId) => deleteOne(port, trackId),
    onMutate: removeOptimistically(queryClient),
    onSuccess: confirmRemoval(queryClient),
    onError: undoFailedRemoval(queryClient),
  });
}

export function useDeleteTrack() {
  const queryClient = useQueryClient();
  const port = useLibraryPort();
  return useAppMutation({
    ...deleteTrackOptions(queryClient, port),
    action: 'library.delete_track',
    trackIdOf: (trackId: TrackId) => trackId,
  });
}
