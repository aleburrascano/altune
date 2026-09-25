import { Alert } from 'react-native';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import { deleteTrack } from '@shared/api-client/tracks';
import { forgetTrack } from '@shared/events/forgetTrack';
import {
  captureTrackPlacements,
  invalidateLibraryDerived,
  restoreTrackPlacements,
} from '@shared/events/trackCachePatch';
import { usePinnedStore } from '@shared/offline/pinnedStore';
import { patchTrackStatus, useTrackStatusStore } from '@shared/acquisition/trackStatusStore';

import { logTrackMutationFailure } from './logTrackMutationFailure';
import { classifyLibraryError, failureTail } from '../state';

const deleteEndpoint = (trackId: TrackId) => `DELETE /v1/tracks/${trackId}`;

export function useDeleteTrack() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (trackId: TrackId) => deleteTrack(trackId),
    onMutate: (trackId: TrackId) => {
      const placements = captureTrackPlacements(queryClient, trackId);
      const status = useTrackStatusStore.getState().statuses[trackId];
      forgetTrack(queryClient, trackId);
      return { placements, status };
    },
    onSuccess: (_data, trackId) => {
      usePinnedStore.getState().unpin(trackId);
      invalidateLibraryDerived(queryClient);
    },
    onError: (error, trackId, context) => {
      const failure = classifyLibraryError(error);
      // Already gone server-side: the optimistic removal was right, so keep it.
      if (failure === 'not-found') {
        usePinnedStore.getState().unpin(trackId);
        return;
      }
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
