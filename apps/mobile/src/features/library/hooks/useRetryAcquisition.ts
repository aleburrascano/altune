import { Alert } from 'react-native';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import { retryAcquisition } from '@shared/api-client/tracks';
import type { TrackResponse } from '@shared/api-client/types';
import { getTrackFromCaches, patchTrackInCaches } from '@shared/events/trackCachePatch';
import { RETRY_TAIL } from '@shared/lib/describeError';

import { logTrackMutationFailure } from './logTrackMutationFailure';

type RetryContext = Pick<TrackResponse, 'acquisition_status' | 'failure_reason'> | undefined;

export function useRetryAcquisition() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (trackId: TrackId) => retryAcquisition(trackId),
    onMutate: (trackId: TrackId): RetryContext => {
      const prior = getTrackFromCaches(queryClient, trackId);
      patchTrackInCaches(queryClient, trackId, {
        acquisition_status: 'pending',
        failure_reason: null,
      });
      return prior
        ? { acquisition_status: prior.acquisition_status, failure_reason: prior.failure_reason }
        : undefined;
    },
    onError: (error, trackId, prior) => {
      logTrackMutationFailure(
        'retry acquisition',
        (id) => `POST /v1/tracks/${id}/retry`,
        trackId,
        error,
      );
      // Roll back the optimistic "pending": the row only offers retry while failed,
      // so leaving it pending would strand the track. Skip if a server event has
      // already moved the track on, so the rollback never overwrites fresher state.
      if (prior && getTrackFromCaches(queryClient, trackId)?.acquisition_status === 'pending') {
        patchTrackInCaches(queryClient, trackId, prior);
      }
      Alert.alert('Retry failed', `Could not restart acquisition. ${RETRY_TAIL}`);
    },
  });
}
