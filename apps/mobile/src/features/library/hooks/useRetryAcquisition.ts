import { Alert } from 'react-native';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { retryAcquisition } from '@shared/api-client/tracks';
import { patchTrackInCaches } from '@shared/events/trackCachePatch';

export function useRetryAcquisition() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (trackId: string) => retryAcquisition(trackId),
    onMutate: (trackId: string) => {
      // Optimistically flip the row back to pending across every cache so the
      // change is realtime, not "wait for a refetch". The re-queued acquisition
      // then emits `track_acquisition_started`, which seeds the download store
      // and drives the phase/completed events as usual.
      patchTrackInCaches(queryClient, trackId, {
        acquisition_status: 'pending',
        failure_reason: null,
      });
    },
    // No onSuccess invalidate (F17): the optimistic pending patch covers every
    // cache, and the re-queued acquisition's SSE events (started → progress →
    // completed/failed) drive the rest — the four refetches were redundant.
    onError: () => {
      Alert.alert('Retry failed', 'Could not restart acquisition. Please try again later.');
    },
  });
}
