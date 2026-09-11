import { Alert } from 'react-native';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import { deleteTrack } from '@shared/api-client/tracks';
import { removeTrackFromCaches } from '@shared/events/trackCachePatch';
import { removeTrackStatus } from '@shared/acquisition/trackStatusStore';
import { RETRY_TAIL } from '@shared/lib/describeError';

export function useDeleteTrack() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (trackId: TrackId) => deleteTrack(trackId),
    onMutate: (trackId: TrackId) => {
      removeTrackFromCaches(queryClient, trackId);
      removeTrackStatus(trackId);
    },
    onError: () => {
      Alert.alert('Delete failed', `Could not remove the track. ${RETRY_TAIL}`);
    },
  });
}

export function useDeleteTracks() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (trackIds: TrackId[]) => {
      let deleted = 0;
      for (const trackId of trackIds) {
        const ok = await deleteTrack(trackId).then(
          () => true,
          () => false,
        );
        if (ok) {
          removeTrackFromCaches(queryClient, trackId);
          removeTrackStatus(trackId);
          deleted += 1;
        }
      }
      return { deleted, requested: trackIds.length };
    },
    onSuccess: ({ deleted, requested }) => {
      if (deleted < requested) {
        Alert.alert(
          'Delete failed',
          `${requested - deleted} of ${requested} tracks could not be removed. ${RETRY_TAIL}`,
        );
      }
    },
  });
}
