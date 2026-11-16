import { useCallback, useEffect, useState } from 'react';
import { useQueryClient, type QueryClient } from '@tanstack/react-query';
import { useAppMutation, libraryKeys } from '@shared/query';
import { showFailureAlert, type FailureAlert } from '@shared/ui';
import { runBounded } from '@shared/concurrency';
import type { TrackId } from '@shared/catalog';
import {
  forgetTrack,
  invalidateLibraryDerived,
  captureTrackPlacements,
  restoreTrackPlacements,
  type TrackCachePlacement,
  TRACK_CACHE_FAMILIES,
} from '@shared/events';
import { usePinnedStore } from '@shared/offline';
import { currentSessionEpoch, isSameSession, guardedMutationOptions } from '@shared/session';
import { useLibraryPort } from '@features/library/view/LibraryPortProvider';
import type { LibraryPort } from '@features/library/ports/libraryPort';
import {
  logTrackMutationFailure,
  failureLogFields,
  libraryFailureAlert,
  classifyLibraryError,
  failureTail,
  type LibraryFailure,
  type LibraryWarn,
} from '@features/library/core/libraryFailure';
import { patchTrackStatus, useTrackStatusStore, type TrackStatus } from '@shared/acquisition';

function deleteEndpoint(trackId: TrackId) {
  return `DELETE /v1/tracks/${trackId}`;
}

const BULK_DELETE_CONCURRENCY = 4;
const BULK_DELETE_DEADLINE_MS = 60_000;

type DeleteTrackFailure = { trackId: TrackId; error: unknown };

type DeleteTracksResult = {
  deleted: number;
  requested: number;
  failures: DeleteTrackFailure[];
  skipped: number;
  cancelled: boolean;
};

type OnDeleted = (trackId: TrackId) => void;
type BatchRun = () => boolean;
type Outcome = { sent: number; failures: DeleteTrackFailure[] };
type RunHandle = { stopped: () => boolean; inStartingSession: () => boolean; finish: () => void };
type RemoveTrack = (trackId: TrackId) => Promise<void>;

function deleteOne(port: LibraryPort, trackId: TrackId): Promise<void> {
  return port.deleteTrack(trackId);
}

type BulkItem = { trackId: TrackId; index: number };
type BulkWorker = (item: BulkItem) => Promise<void>;

async function attemptDelete(remove: RemoveTrack, item: BulkItem) {
  try {
    await remove(item.trackId);
  } catch (error) {
    if (classifyLibraryError(error) !== 'not-found') return { trackId: item.trackId, error };
  }
  return undefined;
}

function bulkWorker(remove: RemoveTrack, onDeleted: OnDeleted, failed: DeleteTrackFailure[]) {
  return async (item: BulkItem): Promise<void> => {
    const failure = await attemptDelete(remove, item);
    if (failure) failed[item.index] = failure;
    else onDeleted(item.trackId);
  };
}

async function deleteInBatches(items: BulkItem[], proceed: BatchRun, worker: BulkWorker) {
  const { succeeded, failed } = await runBounded(items, BULK_DELETE_CONCURRENCY, worker, proceed);
  return succeeded.length + failed.length;
}

type CleanupState = { failed: boolean; warn: LibraryWarn };
function guardCleanup(state: CleanupState, trackId: TrackId, step: () => void): void {
  try {
    step();
  } catch (error) {
    state.failed = true;
    state.warn('[library] delete cleanup failed', { trackId, ...failureLogFields(error) });
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
  let expired = false;
  const timer = setTimeout(() => (expired = true), BULK_DELETE_DEADLINE_MS);
  const stopped = () => handle.stopped() || !handle.inStartingSession();
  function done(): void {
    clearTimeout(timer);
    handle.finish();
  }
  return { run: () => !stopped() && !expired, done };
}

async function runBulkDelete(
  queryClient: QueryClient,
  handle: RunHandle,
  trackIds: TrackId[],
  port: LibraryPort,
): Promise<DeleteTracksResult> {
  const { run, done } = openTimedRun(handle);
  const cleanup: CleanupState = { failed: false, warn: port.warn };
  const onDeleted = removeTrackEverywhere(queryClient, handle, cleanup);
  const failures: DeleteTrackFailure[] = [];
  const items = trackIds.map((trackId, index) => ({ trackId, index }));
  const worker = bulkWorker((id) => deleteOne(port, id), onDeleted, failures);
  const sent = await deleteInBatches(items, run, worker).finally(done);
  if (cleanup.failed && handle.inStartingSession()) resyncTrackCaches(queryClient);
  return summarizeRun(trackIds, { sent, failures: failures.filter(Boolean) }, handle.stopped());
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
  const [stops] = useState(() => new Set<() => void>());
  useEffect(() => {
    return () => stops.forEach((stop) => stop());
  }, [stops]);
  return useCallback(() => createRunHandle(stops), [stops]);
}

function logBulkFailure(warn: LibraryWarn) {
  return ({ trackId, error }: DeleteTrackFailure): void =>
    logTrackMutationFailure(warn, 'delete track', deleteEndpoint, trackId, error);
}

function dominantFailure(failures: DeleteTrackFailure[]): LibraryFailure {
  const kinds = failures.map(({ error }) => classifyLibraryError(error));
  return kinds.includes('auth') ? 'auth' : kinds[0]!;
}

function bulkFailureMessage(requested: number, deleted: number, failures: DeleteTrackFailure[]) {
  const tail = failureTail(dominantFailure(failures));
  return `${requested - deleted} of ${requested} tracks could not be removed. ${tail}`;
}

function bulkFailureAlert({ requested, deleted, failures }: DeleteTracksResult): FailureAlert {
  return {
    surface: 'library.delete_tracks',
    title: 'Delete failed',
    message: bulkFailureMessage(requested, deleted, failures),
  };
}

function reportBulkOutcome(queryClient: QueryClient, warn: LibraryWarn, run: DeleteTracksResult) {
  const { deleted, requested, failures, cancelled } = run;
  if (deleted > 0) invalidateLibraryDerived(queryClient);
  failures.forEach(logBulkFailure(warn));
  if (cancelled || deleted === requested) return;
  showFailureAlert(bulkFailureAlert(run));
}

const LOGGED_TRACK_IDS = 20;
function logBulkRunFailure(warn: LibraryWarn, error: unknown, trackIds: TrackId[]): void {
  warn('[library] bulk delete failed', {
    requested: trackIds.length,
    trackIds: trackIds.slice(0, LOGGED_TRACK_IDS),
    ...failureLogFields(error),
  });
}

function recoverFailedBulkRun(queryClient: QueryClient, warn: LibraryWarn) {
  return (error: Error, trackIds: TrackId[]): void => {
    invalidateLibraryDerived(queryClient);
    void queryClient.invalidateQueries({ queryKey: libraryKeys.tracksPrefix });
    logBulkRunFailure(warn, error, trackIds);
    showFailureAlert(deleteFailedAlert('library.delete_tracks', BULK_RUN_FAILED, error));
  };
}

const BULK_RUN_FAILED = 'Could not remove these tracks.';
const TRACK_DELETE_FAILED = 'Could not remove the track.';

function deleteFailedAlert(surface: string, lead: string, error: unknown): FailureAlert {
  return libraryFailureAlert(surface, 'Delete failed', lead, classifyLibraryError(error));
}

export function useDeleteTracks() {
  const queryClient = useQueryClient();
  const startRun = useUnmountStop();
  const port = useLibraryPort();
  return useAppMutation({
    ...guardedMutationOptions({
      mutationFn: (trackIds: TrackId[]) => runBulkDelete(queryClient, startRun(), trackIds, port),
      onSuccess: (summary: DeleteTracksResult) =>
        reportBulkOutcome(queryClient, port.warn, summary),
      onError: recoverFailedBulkRun(queryClient, port.warn),
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

function undoFailedRemoval(queryClient: QueryClient, warn: LibraryWarn) {
  return (error: Error, trackId: TrackId, removed: RemovedTrack): void => {
    const failure = classifyLibraryError(error);
    if (failure === 'not-found') return unpin(trackId);
    logTrackMutationFailure(warn, 'delete track', deleteEndpoint, trackId, error);
    putTrackBack(queryClient, trackId, removed);
    showFailureAlert(deleteFailedAlert('library.delete_track', TRACK_DELETE_FAILED, error));
  };
}

function deleteTrackOptions(queryClient: QueryClient, port: LibraryPort) {
  return guardedMutationOptions({
    mutationFn: (trackId: TrackId) => deleteOne(port, trackId),
    onMutate: removeOptimistically(queryClient),
    onSuccess: confirmRemoval(queryClient),
    onError: undoFailedRemoval(queryClient, port.warn),
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
