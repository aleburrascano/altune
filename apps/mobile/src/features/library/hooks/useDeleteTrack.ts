import { Alert } from 'react-native';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { deleteTrack } from '@shared/api-client/tracks';
import { removeTrackFromCaches } from '@shared/events/trackCachePatch';
import { removeTrackStatus } from '@shared/acquisition/trackStatusStore';

export function useDeleteTrack() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (trackId: string) => deleteTrack(trackId),
    onMutate: (trackId: string) => {
      removeTrackFromCaches(queryClient, trackId);
      removeTrackStatus(trackId);
    },
    onError: () => {
      Alert.alert('Delete failed', 'Could not remove the track. Please try again.');
    },
  });
}

export function useDeleteTracks() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (trackIds: string[]) => {
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
          `${requested - deleted} of ${requested} tracks could not be removed. Please try again.`,
        );
      }
    },
  });
}
