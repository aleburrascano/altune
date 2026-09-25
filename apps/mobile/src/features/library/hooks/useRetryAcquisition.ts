import { Alert } from 'react-native';
import { useMutation, useQueryClient, type QueryClient } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import { isSafeId } from '@shared/api-client/ids';
import { retryAcquisition } from '@shared/api-client/tracks';
import {
  acquisitionOf,
  toPending,
  type AcquisitionTransition,
} from '@shared/api-client/trackAcquisition';
import {
  recordRetryRequest,
  recordRetryTapped,
  retryFailureOutcome,
  type RetryEntryPoint,
  type RetrySkipReason,
} from '@shared/acquisition/acquisitionTelemetry';
import { getTrackFromCaches, patchTrackInCaches } from '@shared/events/trackCachePatch';
import { guardedMutationOptions } from '@shared/session/signOutCleanup';

import { dropVanishedTrack } from './dropVanishedTrack';
import { logTrackMutationFailure } from './logTrackMutationFailure';
import { trackMutationKeys, useOneRunPerTrack, type TrackMutation } from './useOneRunPerTrack';
import { classifyLibraryError, failureTail } from '../state';

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

function recoverFailedRetry(queryClient: QueryClient, entryPoint: RetryEntryPoint | undefined) {
  return (error: Error, trackId: TrackId, context: PriorAcquisition): void => {
    recordRetryRequest(trackId, entryPoint, retryFailureOutcome(error));
    const failure = classifyLibraryError(error);
    if (failure === 'not-found') return dropVanishedTrack(queryClient, trackId);
    logTrackMutationFailure('retry acquisition', retryEndpoint, trackId, error);
    restorePrior(queryClient, trackId, context);
    Alert.alert('Retry failed', `Could not restart acquisition. ${failureTail(failure)}`);
  };
}

function sendRetry(trackId: TrackId, entryPoint: RetryEntryPoint | undefined): Promise<void> {
  recordRetryRequest(trackId, entryPoint, { kind: 'sent' });
  return retryAcquisition(trackId).then((response) => {
    recordRetryRequest(trackId, entryPoint, { kind: 'succeeded' });
    return response;
  });
}

function retryOptions(queryClient: QueryClient, entryPoint: RetryEntryPoint | undefined) {
  return guardedMutationOptions({
    mutationFn: (trackId: TrackId) => sendRetry(trackId, entryPoint),
    onMutate: markPending(queryClient),
    onError: recoverFailedRetry(queryClient, entryPoint),
  });
}

function skipReason(trackId: TrackId, isInFlight: InFlightCheck): RetrySkipReason | undefined {
  if (!isSafeId(trackId)) return 'unsafe_id';
  if (isInFlight(trackId)) return 'already_running';
  return undefined;
}

function guardedTap(trackId: TrackId, entryPoint: EntryPoint, isInFlight: InFlightCheck, mutate: MutateFn): void {
  recordRetryTapped(trackId, entryPoint);
  const reason = skipReason(trackId, isInFlight);
  if (reason) return recordRetryRequest(trackId, entryPoint, { kind: 'skipped', reason });
  mutate(trackId);
}

function guardedMutate(entryPoint: EntryPoint, run: TrackMutation<void, RetryContext>) {
  return (trackId: TrackId) => guardedTap(trackId, entryPoint, run.isInFlight, run.mutate);
}

function useRetryRun(entryPoint: EntryPoint) {
  const queryClient = useQueryClient();
  const mutation = useMutation({
    mutationKey: trackMutationKeys.retryAcquisition,
    ...retryOptions(queryClient, entryPoint),
  });
  return useOneRunPerTrack(mutation, trackMutationKeys.retryAcquisition);
}

export function useRetryAcquisition(entryPoint?: RetryEntryPoint): TrackMutation<void, RetryContext> {
  const run = useRetryRun(entryPoint);
  return { ...run, mutate: guardedMutate(entryPoint, run) };
}
