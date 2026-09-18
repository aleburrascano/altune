import { useCallback, useEffect, useRef } from 'react';
import { Alert } from 'react-native';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import { deleteTrack } from '@shared/api-client/tracks';
import {
  captureTrackPlacements,
  invalidateLibraryDerived,
  removeTrackFromCaches,
  restoreTrackPlacements,
} from '@shared/events/trackCachePatch';
import {
  patchTrackStatus,
  removeTrackStatus,
  useTrackStatusStore,
} from '@shared/acquisition/trackStatusStore';
import { RETRY_TAIL } from '@shared/lib/describeError';

import { logTrackMutationFailure } from './logTrackMutationFailure';
import { classifyLibraryError, failureTail } from '../state';

const deleteEndpoint = (trackId: TrackId) => `DELETE /v1/tracks/${trackId}`;

export type DeleteTrackFailure = { trackId: TrackId; error: unknown };

export function useDeleteTrack() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (trackId: TrackId) => deleteTrack(trackId),
    onMutate: (trackId: TrackId) => {
      const placements = captureTrackPlacements(queryClient, trackId);
      const status = useTrackStatusStore.getState().statuses[trackId];
      removeTrackFromCaches(queryClient, trackId);
      removeTrackStatus(trackId);
      return { placements, status };
    },
    onSuccess: () => invalidateLibraryDerived(queryClient),
    onError: (error, trackId, context) => {
      const failure = classifyLibraryError(error);
      // Already gone server-side: the optimistic removal was right, so keep it.
      if (failure === 'not-found') return;
      logTrackMutationFailure('delete track', deleteEndpoint, trackId, error);
      // The track still exists server-side; put it back where it was, since these
      // caches never refetch on their own.
      if (context) {
        restoreTrackPlacements(queryClient, context.placements);
        if (context.status) patchTrackStatus(trackId, context.status);
      }
      Alert.alert('Delete failed', `Could not remove the track. ${failureTail(failure)}`);
    },
  });
}

/** Deletes sent at once; a dead dependency costs one timeout per slot, not per track. */
export const BULK_DELETE_CONCURRENCY = 4;
/**
 * No new delete starts after this; the run then ends within one request timeout.
 * Sized so a healthy API clears a large "select all" long before it binds.
 */
export const BULK_DELETE_DEADLINE_MS = 60_000;

export type DeleteTracksResult = {
  deleted: number;
  requested: number;
  failures: DeleteTrackFailure[];
  /** Tracks never sent because the run hit its deadline or its screen unmounted. */
  skipped: number;
  cancelled: boolean;
};

type BatchRun = { stopped: () => boolean; expired: () => boolean };

/**
 * Sends the deletes through a fixed pool of workers, each taking the next unsent
 * track until the list is exhausted, the deadline passes, or the run is stopped.
 * `onDeleted` fires per confirmed delete; failures keep their input order.
 */
async function deleteInBatches(
  trackIds: TrackId[],
  run: BatchRun,
  onDeleted: (trackId: TrackId) => void,
): Promise<{ sent: number; failures: DeleteTrackFailure[] }> {
  let next = 0;
  const failed: (DeleteTrackFailure | undefined)[] = [];
  const worker = async (): Promise<void> => {
    while (next < trackIds.length && !run.stopped() && !run.expired()) {
      const index = next++;
      const trackId = trackIds[index]!;
      await deleteTrack(trackId).then(
        () => onDeleted(trackId),
        (error: unknown) =>
          // A track that is already gone is as deleted as the user asked for.
          classifyLibraryError(error) === 'not-found'
            ? onDeleted(trackId)
            : (failed[index] = { trackId, error }),
      );
    }
  };
  const workers = Math.min(BULK_DELETE_CONCURRENCY, trackIds.length);
  await Promise.all(Array.from({ length: workers }, worker));
  return { sent: next, failures: failed.filter((f): f is DeleteTrackFailure => f != null) };
}

/**
 * Keeps a bulk delete from sending anything more once the owning screen unmounts.
 * The deletes already in flight are left to finish: cancelling one would not
 * un-delete the track server-side, and only its result can take that track out of
 * caches the whole app reads. Each run hands its stop back through `finish`.
 */
function useUnmountStop(): () => { stopped: () => boolean; finish: () => void } {
  const stops = useRef(new Set<() => void>());
  useEffect(() => {
    const active = stops.current;
    return () => active.forEach((stop) => stop());
  }, []);
  return useCallback(() => {
    let stopped = false;
    const stop = () => (stopped = true);
    stops.current.add(stop);
    return { stopped: () => stopped, finish: () => stops.current.delete(stop) };
  }, []);
}

export function useDeleteTracks() {
  const queryClient = useQueryClient();
  const startRun = useUnmountStop();
  return useMutation({
    mutationFn: async (trackIds: TrackId[]): Promise<DeleteTracksResult> => {
      const run = startRun();
      let expired = false;
      const timer = setTimeout(() => (expired = true), BULK_DELETE_DEADLINE_MS);
      const { sent, failures } = await deleteInBatches(
        trackIds,
        { stopped: run.stopped, expired: () => expired },
        (trackId) => {
          removeTrackFromCaches(queryClient, trackId);
          removeTrackStatus(trackId);
        },
      ).finally(() => {
        clearTimeout(timer);
        run.finish();
      });
      return {
        deleted: sent - failures.length,
        requested: trackIds.length,
        failures,
        skipped: trackIds.length - sent,
        cancelled: run.stopped(),
      };
    },
    onSuccess: ({ deleted, requested, failures, cancelled }) => {
      if (deleted > 0) invalidateLibraryDerived(queryClient);
      for (const { trackId, error } of failures) {
        logTrackMutationFailure('delete track', deleteEndpoint, trackId, error);
      }
      // The user left the screen that asked for this; don't pop an alert elsewhere.
      if (cancelled || deleted === requested) return;
      Alert.alert(
        'Delete failed',
        `${requested - deleted} of ${requested} tracks could not be removed. ${RETRY_TAIL}`,
      );
    },
  });
}
