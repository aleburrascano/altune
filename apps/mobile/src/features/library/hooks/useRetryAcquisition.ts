import { Alert } from 'react-native';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import { retryAcquisition } from '@shared/api-client/tracks';
import { patchTrackInCaches } from '@shared/events/trackCachePatch';

export function useRetryAcquisition() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (trackId: TrackId) => retryAcquisition(trackId),
    onMutate: (trackId: TrackId) => {
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
