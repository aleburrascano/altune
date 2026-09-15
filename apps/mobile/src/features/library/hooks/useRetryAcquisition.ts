import { Alert } from 'react-native';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import { retryAcquisition } from '@shared/api-client/tracks';
import {
  acquisitionOf,
  toPending,
  type AcquisitionTransition,
} from '@shared/api-client/trackAcquisition';
import { getTrackFromCaches, patchTrackInCaches } from '@shared/events/trackCachePatch';

import { dropVanishedTrack } from './dropVanishedTrack';
import { logTrackMutationFailure } from './logTrackMutationFailure';
import { classifyLibraryError, failureTail } from '../state';

type RetryContext = AcquisitionTransition | undefined;

export function useRetryAcquisition() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (trackId: TrackId) => retryAcquisition(trackId),
    onMutate: (trackId: TrackId): RetryContext => {
      const prior = getTrackFromCaches(queryClient, trackId);
      patchTrackInCaches(queryClient, trackId, toPending());
      return prior ? acquisitionOf(prior) : undefined;
    },
    onError: (error, trackId, prior) => {
      const failure = classifyLibraryError(error);
      // The track was deleted elsewhere: there is nothing to roll back to or retry.
      if (failure === 'not-found') return dropVanishedTrack(queryClient, trackId);
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
      Alert.alert('Retry failed', `Could not restart acquisition. ${failureTail(failure)}`);
    },
  });
}
