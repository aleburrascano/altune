import { Alert } from 'react-native';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import { deleteTrack } from '@shared/api-client/tracks';
import {
  captureTrackPlacements,
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
    onError: (error, trackId, context) => {
      logTrackMutationFailure('delete track', deleteEndpoint, trackId, error);
      // The track still exists server-side; put it back where it was, since these
      // caches never refetch on their own.
      if (context) {
        restoreTrackPlacements(queryClient, context.placements);
        if (context.status) patchTrackStatus(trackId, context.status);
      }
      Alert.alert('Delete failed', `Could not remove the track. ${RETRY_TAIL}`);
    },
  });
}

export function useDeleteTracks() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (trackIds: TrackId[]) => {
      let deleted = 0;
      const failures: DeleteTrackFailure[] = [];
      for (const trackId of trackIds) {
        const failure = await deleteTrack(trackId).then(
          () => null,
          (error: unknown) => ({ trackId, error }),
        );
        if (failure) {
          failures.push(failure);
        } else {
          removeTrackFromCaches(queryClient, trackId);
          removeTrackStatus(trackId);
          deleted += 1;
        }
      }
      return { deleted, requested: trackIds.length, failures };
    },
    onSuccess: ({ deleted, requested, failures }) => {
      for (const { trackId, error } of failures) {
        logTrackMutationFailure('delete track', deleteEndpoint, trackId, error);
      }
      if (deleted < requested) {
        Alert.alert(
          'Delete failed',
          `${requested - deleted} of ${requested} tracks could not be removed. ${RETRY_TAIL}`,
        );
      }
    },
  });
}
