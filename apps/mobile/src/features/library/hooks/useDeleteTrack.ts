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
