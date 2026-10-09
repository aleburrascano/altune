import { useMemo } from 'react';
import {
  useMutationState,
  useQueryClient,
  type Mutation,
  type MutationCache,
  type MutationFilters,
  type UseMutationResult,
  type QueryClient,
} from '@tanstack/react-query';
import { type TrackId, isSafeId } from '@shared/api-client/ids';
import { useAppMutation } from '@shared/query/useAppMutation';
import { forgetTrack } from '@shared/events/forgetTrack';
import { showFailureAlert } from '@shared/ui';
import {
  toPending,
  acquisitionOf,
  type AcquisitionTransition,
} from '@shared/api-client/trackAcquisition';
import { patchTrackInCaches } from '@shared/events/trackPatchBatch';
import { guardedMutationOptions } from '@shared/session/signOutCleanup';
import { useLibraryPort } from '@features/library/ports/LibraryPortProvider';
import type { LibraryPort } from '@features/library/ports/libraryPort';
import {
  trackNotFoundAlert,
  logTrackMutationFailure,
  libraryFailureAlert,
  classifyLibraryError,
} from '@features/library/domain/libraryFailure';
import {
  recordRetryRequest,
  recordRetryTapped,
  type RetryEntryPoint,
  type RetrySkipReason,
} from '@shared/acquisition/acquisitionTelemetry';
import { getTrackFromCaches } from '@shared/events/trackCachePatch';
import { useSharedRetryAcquisition } from '@shared/acquisition/useRetryAcquisition';

export const trackMutationKeys = {
  retryAcquisition: ['track', 'retry-acquisition'] as const,
  reacquire: ['track', 'reacquire'] as const,
};

type TrackMutationKey = (typeof trackMutationKeys)[keyof typeof trackMutationKeys];

export type TrackMutation<TData, TContext> = Omit<
  UseMutationResult<TData, Error, TrackId, TContext>,
  'mutate' | 'mutateAsync'
> & {
  mutate: (trackId: TrackId) => void;
  isInFlight: (trackId: TrackId) => boolean;
};

const pendingRunsOf = (mutationKey: TrackMutationKey): MutationFilters => ({
  mutationKey,
  status: 'pending',
});

const trackIdOf = (run: Mutation<unknown, Error, unknown, unknown>): TrackId =>
  run.state.variables as TrackId;

function hasPendingRun(
  cache: MutationCache,
  mutationKey: TrackMutationKey,
  trackId: TrackId,
): boolean {
  return cache.findAll(pendingRunsOf(mutationKey)).some((run) => trackIdOf(run) === trackId);
}

export function useOneRunPerTrack<TData, TContext>(
  mutation: UseMutationResult<TData, Error, TrackId, TContext>,
  mutationKey: TrackMutationKey,
): TrackMutation<TData, TContext> {
  const mutationCache = useQueryClient().getMutationCache();
  const pendingIds = useMutationState({ filters: pendingRunsOf(mutationKey), select: trackIdOf });
  const inFlight = useMemo(() => new Set<TrackId>(pendingIds), [pendingIds]);
  const { mutate, ...run } = mutation;

  return {
    ...run,
    mutate: (trackId: TrackId) => {
      if (hasPendingRun(mutationCache, mutationKey, trackId)) return;
      mutate(trackId);
    },
    isInFlight: (trackId: TrackId) => inFlight.has(trackId),
  };
}

const reacquireEndpoint = (trackId: TrackId) => `POST /v1/tracks/${trackId}/reacquire`;

const REACQUIRE_FAILED = 'Could not start a re-acquisition. Your current audio is unchanged.';

function markReacquirePending(queryClient: QueryClient) {
  return (_started: void, trackId: TrackId): void => {
    patchTrackInCaches(queryClient, trackId, toPending());
  };
}

function reportFailedReacquire(queryClient: QueryClient) {
  return (error: Error, trackId: TrackId): void => {
    const failure = classifyLibraryError(error);
    if (failure === 'not-found') {
      forgetTrack(queryClient, trackId);
      return showFailureAlert(trackNotFoundAlert(trackId));
    }
    logTrackMutationFailure('re-acquire track', reacquireEndpoint, trackId, error);
    showFailureAlert(
      libraryFailureAlert('library.reacquire', 'Re-acquire failed', REACQUIRE_FAILED, failure),
    );
  };
}

function reacquireOptions(queryClient: QueryClient, port: LibraryPort) {
  return guardedMutationOptions({
    mutationFn: (trackId: TrackId) => port.reacquireTrack(trackId),
    onSuccess: markReacquirePending(queryClient),
    onError: reportFailedReacquire(queryClient),
  });
}

export function useReacquireTrack(): TrackMutation<void, unknown> {
  const queryClient = useQueryClient();
  const port = useLibraryPort();
  const mutation = useAppMutation({
    action: 'library.reacquire',
    trackIdOf: (trackId: TrackId) => trackId,
    mutationKey: trackMutationKeys.reacquire,
    ...reacquireOptions(queryClient, port),
  });
  return useOneRunPerTrack(mutation, trackMutationKeys.reacquire);
}

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
