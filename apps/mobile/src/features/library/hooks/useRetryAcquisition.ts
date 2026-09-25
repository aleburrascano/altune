import { Alert } from 'react-native';
import { useMutation, useQueryClient, type QueryClient } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import { retryAcquisition } from '@shared/api-client/tracks';
import {
  acquisitionOf,
  toPending,
  type AcquisitionTransition,
} from '@shared/api-client/trackAcquisition';
import { getTrackFromCaches, patchTrackInCaches } from '@shared/events/trackCachePatch';
import { guardedMutationOptions } from '@shared/session/signOutCleanup';

import { dropVanishedTrack } from './dropVanishedTrack';
import { logTrackMutationFailure } from './logTrackMutationFailure';
import { trackMutationKeys, useOneRunPerTrack, type TrackMutation } from './useOneRunPerTrack';
import { classifyLibraryError, failureTail } from '../state';

type PriorAcquisition = { prior: AcquisitionTransition | undefined };
type RetryContext = PriorAcquisition & { epoch: number };

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
    if (failure === 'not-found') return dropVanishedTrack(queryClient, trackId);
    logTrackMutationFailure('retry acquisition', retryEndpoint, trackId, error);
    restorePrior(queryClient, trackId, context);
    Alert.alert('Retry failed', `Could not restart acquisition. ${failureTail(failure)}`);
  };
}

function retryOptions(queryClient: QueryClient) {
  return guardedMutationOptions({
    mutationFn: (trackId: TrackId) => retryAcquisition(trackId),
    onMutate: markPending(queryClient),
    onError: recoverFailedRetry(queryClient),
  });
}

export function useRetryAcquisition(): TrackMutation<void, RetryContext> {
  const queryClient = useQueryClient();
  const mutation = useMutation({
    mutationKey: trackMutationKeys.retryAcquisition,
    ...retryOptions(queryClient),
  });
  return useOneRunPerTrack(mutation, trackMutationKeys.retryAcquisition);
}
