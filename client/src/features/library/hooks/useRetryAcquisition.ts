import { useQueryClient, type QueryClient } from '@tanstack/react-query';

import { forgetTrack } from '@shared/events/forgetTrack';
import { showFailureAlert } from '@shared/ui';
import type { TrackId } from '@shared/api-client/ids';
import { isSafeId } from '@shared/api-client/ids';
import {
  acquisitionOf,
  toPending,
  type AcquisitionTransition,
} from '@shared/api-client/trackAcquisition';
import {
  recordRetryRequest,
  recordRetryTapped,
  type RetryEntryPoint,
  type RetrySkipReason,
} from '@shared/acquisition/acquisitionTelemetry';
import { getTrackFromCaches } from '@shared/events/trackCachePatch';
import { patchTrackInCaches } from '@shared/events/trackPatchBatch';
import { useSharedRetryAcquisition } from '@shared/acquisition/useRetryAcquisition';

import { trackNotFoundAlert } from '@features/library/domain/trackNotFoundAlert';
import { logTrackMutationFailure } from '@features/library/domain/logTrackMutationFailure';
import {
  trackMutationKeys,
  useOneRunPerTrack,
  type TrackMutation,
} from '@features/library/hooks/useOneRunPerTrack';
import { libraryFailureAlert } from '@features/library/domain/libraryFailureAlert';
import { classifyLibraryError } from '@features/library/domain/state';

type PriorAcquisition = { prior: AcquisitionTransition | undefined };
type RetryContext = PriorAcquisition & { epoch: number };
type EntryPoint = RetryEntryPoint | undefined;
type InFlightCheck = (trackId: TrackId) => boolean;
type MutateFn = (trackId: TrackId) => void;

const retryEndpoint = (trackId: TrackId) => `POST /v1/tracks/${trackId}/retry`;

function markPending(queryClient: QueryClient) {
  return (trackId: TrackId): PriorAcquisition => {
    const track = getTrackFromCaches(queryClient, trackId);
    patchTrackInCaches(queryClient, trackId, toPending());
    return { prior: track ? acquisitionOf(track) : undefined };
  };
}

function isStillPending(queryClient: QueryClient, trackId: TrackId): boolean {
  return getTrackFromCaches(queryClient, trackId)?.acquisition_status === 'pending';
}

function restorePrior(queryClient: QueryClient, trackId: TrackId, { prior }: PriorAcquisition) {
  if (prior && isStillPending(queryClient, trackId)) {
    patchTrackInCaches(queryClient, trackId, prior);
  }
}

function recoverFailedRetry(queryClient: QueryClient) {
  return (error: Error, trackId: TrackId, context: PriorAcquisition): void => {
    const failure = classifyLibraryError(error);
    if (failure === 'not-found') {
      forgetTrack(queryClient, trackId);
      return showFailureAlert(trackNotFoundAlert(trackId));
    }
    logTrackMutationFailure('retry acquisition', retryEndpoint, trackId, error);
    restorePrior(queryClient, trackId, context);
    showFailureAlert(
      libraryFailureAlert(
        'library.retry',
        'Retry failed',
        'Could not restart acquisition.',
        failure,
      ),
    );
  };
}

function skipReason(trackId: TrackId, isInFlight: InFlightCheck): RetrySkipReason | undefined {
  if (!isSafeId(trackId)) return 'unsafe_id';
  if (isInFlight(trackId)) return 'already_running';
  return undefined;
}

type GuardedTapArgs = {
  trackId: TrackId;
  entryPoint: EntryPoint;
  isInFlight: InFlightCheck;
  mutate: MutateFn;
};

function guardedTap({ trackId, entryPoint, isInFlight, mutate }: GuardedTapArgs): void {
  recordRetryTapped(trackId, entryPoint);
  const reason = skipReason(trackId, isInFlight);
  if (reason) return recordRetryRequest(trackId, entryPoint, { kind: 'skipped', reason });
  mutate(trackId);
}

function guardedMutate(entryPoint: EntryPoint, run: TrackMutation<void, RetryContext>) {
  return (trackId: TrackId) =>
    guardedTap({ trackId, entryPoint, isInFlight: run.isInFlight, mutate: run.mutate });
}

function useRetryRun(entryPoint: EntryPoint) {
  const queryClient = useQueryClient();
  const mutation = useSharedRetryAcquisition({
    action: 'library.retry',
    entryPoint,
    mutationKey: trackMutationKeys.retryAcquisition,
    onMutate: markPending(queryClient),
    onError: recoverFailedRetry(queryClient),
  });
  return useOneRunPerTrack(mutation, trackMutationKeys.retryAcquisition);
}

export function useRetryAcquisition(
  entryPoint?: RetryEntryPoint,
): TrackMutation<void, RetryContext> {
  const run = useRetryRun(entryPoint);
  return { ...run, mutate: guardedMutate(entryPoint, run) };
}
