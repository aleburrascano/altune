import { Alert } from 'react-native';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { reacquireTrack } from '@shared/api-client/tracks';
import { patchTrackInCaches } from '@shared/events/trackCachePatch';

export function useReacquireTrack() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (trackId: string) => reacquireTrack(trackId),
    onSuccess: (_data, trackId) => {
      patchTrackInCaches(queryClient, trackId, { acquisition_status: 'pending' });
    },
    onError: () => {
      Alert.alert(
        'Re-acquire failed',
        'Could not start a re-acquisition. Your current audio is unchanged.',
      );
    },
  });
}
