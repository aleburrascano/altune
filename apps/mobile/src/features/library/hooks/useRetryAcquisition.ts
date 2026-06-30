import { Alert } from 'react-native';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { clearTrackStage } from '@shared/acquisition/stageStore';
import { retryAcquisition } from '@shared/api-client/tracks';
import { patchTrackInCaches } from '@shared/events/trackCachePatch';

export function useRetryAcquisition() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (trackId: string) => retryAcquisition(trackId),
    onMutate: (trackId: string) => {
      // Optimistically flip the row back to pending across every cache so the
      // change is realtime, not "wait for a refetch". The re-queued acquisition
      // then drives stage/completed events as usual.
      clearTrackStage(trackId);
      patchTrackInCaches(queryClient, trackId, {
        acquisition_status: 'pending',
        failure_reason: null,
      });
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['library-home'] });
      void queryClient.invalidateQueries({ queryKey: ['library'] });
      void queryClient.invalidateQueries({ queryKey: ['playlist'] });
      void queryClient.invalidateQueries({ queryKey: ['playlists'] });
    },
    onError: () => {
      Alert.alert('Retry failed', 'Could not restart acquisition. Please try again later.');
    },
  });
}
