import { useCallback, useEffect, useRef } from 'react';
import { Alert } from 'react-native';
import { useMutation, useQueryClient, type QueryClient } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import { deleteTrack } from '@shared/api-client/tracks';
import { invalidateLibraryDerived, removeTrackFromCaches } from '@shared/events/trackCachePatch';
import { removeTrackStatus } from '@shared/acquisition/trackStatusStore';
import { RETRY_TAIL } from '@shared/lib/describeError';

import { logTrackMutationFailure } from './logTrackMutationFailure';
import { classifyLibraryError } from '../state';

const deleteEndpoint = (trackId: TrackId) => `DELETE /v1/tracks/${trackId}`;

export const BULK_DELETE_CONCURRENCY = 4;
export const BULK_DELETE_DEADLINE_MS = 60_000;

export type DeleteTrackFailure = { trackId: TrackId; error: unknown };

export type DeleteTracksResult = {
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
type RunHandle = { stopped: () => boolean; finish: () => void };
type Deadline = { expired: () => boolean; clear: () => void };
type Cursor = { taken: number };
type BatchContext = {
  cursor: Cursor;
  trackIds: TrackId[];
  run: BatchRun;
  onDeleted: OnDeleted;
  failed: DeleteAttempt[];
};

async function deleteTrackForBulk(trackId: TrackId, onDeleted: OnDeleted): Promise<DeleteAttempt> {
  try {
    await deleteTrack(trackId);
  } catch (error) {
    if (classifyLibraryError(error) !== 'not-found') return { trackId, error };
  }
  onDeleted(trackId);
  return undefined;
}

function takeNextIndex(cursor: Cursor, trackIds: TrackId[], run: BatchRun): number | undefined {
  if (cursor.taken >= trackIds.length || run.stopped() || run.expired()) return undefined;
  return cursor.taken++;
}

async function drainWorker(ctx: BatchContext): Promise<void> {
  let index = takeNextIndex(ctx.cursor, ctx.trackIds, ctx.run);
  while (index !== undefined) {
    const failure = await deleteTrackForBulk(ctx.trackIds[index]!, ctx.onDeleted);
    if (failure) ctx.failed[index] = failure;
    index = takeNextIndex(ctx.cursor, ctx.trackIds, ctx.run);
  }
}

function collectFailures(failed: DeleteAttempt[]): DeleteTrackFailure[] {
  return failed.filter((failure): failure is DeleteTrackFailure => failure != null);
}

function newContext(trackIds: TrackId[], run: BatchRun, onDeleted: OnDeleted): BatchContext {
  return { cursor: { taken: 0 }, trackIds, run, onDeleted, failed: [] };
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

function removeTrackEverywhere(queryClient: QueryClient): OnDeleted {
  return (trackId) => {
    removeTrackFromCaches(queryClient, trackId);
    removeTrackStatus(trackId);
  };
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
  const run: BatchRun = { stopped: handle.stopped, expired: deadline.expired };
  return { run, done: finalizeRun(deadline, handle) };
}

async function runBulkDelete(
  queryClient: QueryClient,
  handle: RunHandle,
  trackIds: TrackId[],
): Promise<DeleteTracksResult> {
  const { run, done } = openTimedRun(handle);
  const ctx = newContext(trackIds, run, removeTrackEverywhere(queryClient));
  const outcome = await deleteInBatches(ctx).finally(done);
  return summarizeRun(trackIds, outcome, handle.stopped());
}

function createRunHandle(stops: Set<() => void>): RunHandle {
  let stopped = false;
  const stop = () => (stopped = true);
  stops.add(stop);
  return { stopped: () => stopped, finish: () => stops.delete(stop) };
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

function bulkFailureMessage(requested: number, deleted: number): string {
  return `${requested - deleted} of ${requested} tracks could not be removed. ${RETRY_TAIL}`;
}

function reportBulkOutcome(queryClient: QueryClient, summary: DeleteTracksResult): void {
  const { deleted, requested, failures, cancelled } = summary;
  if (deleted > 0) invalidateLibraryDerived(queryClient);
  failures.forEach(logBulkFailure);
  if (cancelled || deleted === requested) return;
  Alert.alert('Delete failed', bulkFailureMessage(requested, deleted));
}

export function useDeleteTracks() {
  const queryClient = useQueryClient();
  const startRun = useUnmountStop();
  return useMutation({
    mutationFn: (trackIds: TrackId[]) => runBulkDelete(queryClient, startRun(), trackIds),
    onSuccess: (summary) => reportBulkOutcome(queryClient, summary),
  });
}
