import { Alert } from 'react-native';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { retryAcquisition } from '@shared/api-client/tracks';
import { patchTrackInCaches } from '@shared/events/trackCachePatch';

export function useRetryAcquisition() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (trackId: string) => retryAcquisition(trackId),
    onMutate: (trackId: string) => {
      patchTrackInCaches(queryClient, trackId, {
        acquisition_status: 'pending',
        failure_reason: null,
      });
    },
    onError: () => {
      Alert.alert('Retry failed', 'Could not restart acquisition. Please try again later.');
    },
  });
}
