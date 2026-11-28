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
import {
  type AcquisitionTransition,
  type TrackId,
  acquisitionOf,
  isPending,
  isSafeId,
  toPending,
} from '@shared/catalog';
import { useAppMutation } from '@shared/query';
import { forgetTrack, getTrackFromCaches, patchTrackInCaches } from '@shared/events';
import type { FailureAlert } from '@shared/ui';
import { guardedMutationOptions } from '@shared/session';
import { useLibraryPort } from '@features/library/view/LibraryProvider';
import type { LibraryPort } from '@features/library/ports/ports';
import {
  trackNotFoundAlert,
  logTrackMutationFailure,
  libraryFailureAlert,
  classifyLibraryError,
  type LibraryFailure,
} from '@features/library/core/errors';
import {
  recordRetryRequest,
  recordRetryTapped,
  type RetryEntryPoint,
  type RetrySkipReason,
  useSharedRetryAcquisition,
} from '@shared/acquisition';
import { trackMutationKeys, type TrackMutationKey } from '@features/library/core/queryKeys';

export type TrackMutation<TData, TContext> = Omit<
  UseMutationResult<TData, Error, TrackId, TContext>,
  'mutate' | 'mutateAsync'
> & {
  mutate: (trackId: TrackId) => void;
  isInFlight: (trackId: TrackId) => boolean;
};

function pendingRunsOf(mutationKey: TrackMutationKey): MutationFilters {
  return { mutationKey, status: 'pending' };
}

function asRunTrackId(run: Mutation): TrackId {
  return run.state.variables as TrackId;
}

function hasPendingRun(
  cache: MutationCache,
  mutationKey: TrackMutationKey,
  trackId: TrackId,
): boolean {
  return cache.findAll(pendingRunsOf(mutationKey)).some((run) => asRunTrackId(run) === trackId);
}

function useMutateUnlessPending(
  mutationKey: TrackMutationKey,
  mutate: (trackId: TrackId) => void,
): (trackId: TrackId) => void {
  const mutationCache = useQueryClient().getMutationCache();
  return (trackId) => {
    if (!hasPendingRun(mutationCache, mutationKey, trackId)) mutate(trackId);
  };
}

function useIsInFlight(mutationKey: TrackMutationKey): (trackId: TrackId) => boolean {
  const pendingIds = useMutationState({
    filters: pendingRunsOf(mutationKey),
    select: asRunTrackId,
  });
  const inFlight = useMemo(() => new Set<TrackId>(pendingIds), [pendingIds]);
  return (trackId) => inFlight.has(trackId);
}

function useOneRunPerTrack<TData, TContext>(
  mutation: UseMutationResult<TData, Error, TrackId, TContext>,
  mutationKey: TrackMutationKey,
): TrackMutation<TData, TContext> {
  const { mutate, ...run } = mutation;
  const mutateOnce = useMutateUnlessPending(mutationKey, mutate);
  const isInFlight = useIsInFlight(mutationKey);
  return { ...run, mutate: mutateOnce, isInFlight };
}

type ReacquireContext = { epoch: number };

function reacquireEndpoint(trackId: TrackId) {
  return `POST /v1/tracks/${trackId}/reacquire`;
}

const REACQUIRE_FAILED = 'Could not start a re-acquisition. Your current audio is unchanged.';

function markReacquirePending(queryClient: QueryClient) {
  return (_started: unknown, trackId: TrackId): void => {
    patchTrackInCaches(queryClient, trackId, toPending());
  };
}

function forgetMissingTrack(queryClient: QueryClient, port: LibraryPort, trackId: TrackId): void {
  forgetTrack(queryClient, trackId);
  port.showFailure(trackNotFoundAlert(trackId));
}

function reacquireFailedAlert(failure: LibraryFailure): FailureAlert {
  return libraryFailureAlert('library.reacquire', 'Re-acquire failed', REACQUIRE_FAILED, failure);
}

function reportFailedReacquire(queryClient: QueryClient, port: LibraryPort) {
  return (error: Error, trackId: TrackId): void => {
    const failure = classifyLibraryError(error);
    if (failure === 'not-found') return forgetMissingTrack(queryClient, port, trackId);
    logTrackMutationFailure(port.warn, 're-acquire track', reacquireEndpoint, trackId, error);
    port.showFailure(reacquireFailedAlert(failure));
  };
}

function reacquireOptions(queryClient: QueryClient, port: LibraryPort) {
  return guardedMutationOptions({
    mutationFn: (trackId: TrackId) => port.reacquireTrack(trackId),
    onSuccess: markReacquirePending(queryClient),
    onError: reportFailedReacquire(queryClient, port),
  });
}

function useReacquireMutation() {
  const queryClient = useQueryClient();
  const port = useLibraryPort();
  return useAppMutation({
    action: 'library.reacquire',
    trackIdOf: (trackId: TrackId) => trackId,
    mutationKey: trackMutationKeys.reacquire,
    ...reacquireOptions(queryClient, port),
  });
}

export function useReacquireTrack(): TrackMutation<void, ReacquireContext> {
  return useOneRunPerTrack(useReacquireMutation(), trackMutationKeys.reacquire);
}

type PriorAcquisition = { prior: AcquisitionTransition | undefined };
type RetryContext = PriorAcquisition & { epoch: number };
type EntryPoint = RetryEntryPoint | undefined;
type InFlightCheck = (trackId: TrackId) => boolean;
type MutateFn = (trackId: TrackId) => void;

function retryEndpoint(trackId: TrackId) {
  return `POST /v1/tracks/${trackId}/retry`;
}

function markPending(queryClient: QueryClient) {
  return (trackId: TrackId): PriorAcquisition => {
    const track = getTrackFromCaches(queryClient, trackId);
    patchTrackInCaches(queryClient, trackId, toPending());
    return { prior: track ? acquisitionOf(track) : undefined };
  };
}

function isStillPending(queryClient: QueryClient, trackId: TrackId): boolean {
  return isPending(getTrackFromCaches(queryClient, trackId)?.acquisition_status);
}

function restorePrior(queryClient: QueryClient, trackId: TrackId, { prior }: PriorAcquisition) {
  if (prior && isStillPending(queryClient, trackId)) {
    patchTrackInCaches(queryClient, trackId, prior);
  }
}

function recoverFailedRetry(queryClient: QueryClient, port: LibraryPort) {
  return (error: Error, trackId: TrackId, context: PriorAcquisition): void => {
    const failure = classifyLibraryError(error);
    if (failure === 'not-found') return forgetMissingTrack(queryClient, port, trackId);
    logTrackMutationFailure(port.warn, 'retry acquisition', retryEndpoint, trackId, error);
    restorePrior(queryClient, trackId, context);
    port.showFailure(retryFailedAlert(failure));
  };
}

function retryFailedAlert(failure: LibraryFailure): FailureAlert {
  return libraryFailureAlert(
    'library.retry',
    'Retry failed',
    'Could not restart acquisition.',
    failure,
  );
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
  const port = useLibraryPort();
  const mutation = useSharedRetryAcquisition({
    action: 'library.retry',
    entryPoint,
    mutationKey: trackMutationKeys.retryAcquisition,
    onMutate: markPending(queryClient),
    onError: recoverFailedRetry(queryClient, port),
  });
  return useOneRunPerTrack(mutation, trackMutationKeys.retryAcquisition);
}

export function useRetryAcquisition(
  entryPoint?: RetryEntryPoint,
): TrackMutation<void, RetryContext> {
  const run = useRetryRun(entryPoint);
  return { ...run, mutate: guardedMutate(entryPoint, run) };
}
