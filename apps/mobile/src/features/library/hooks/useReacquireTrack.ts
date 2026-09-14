import { Alert } from 'react-native';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import { reacquireTrack } from '@shared/api-client/tracks';
import { patchTrackInCaches } from '@shared/events/trackCachePatch';
import { RETRY_TAIL } from '@shared/lib/describeError';

import { logTrackMutationFailure } from './logTrackMutationFailure';

export function useReacquireTrack() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (trackId: TrackId) => reacquireTrack(trackId),
    onSuccess: (_data, trackId) => {
      patchTrackInCaches(queryClient, trackId, { acquisition_status: 'pending' });
    },
    onError: (error, trackId) => {
      logTrackMutationFailure(
        're-acquire track',
        (id) => `POST /v1/tracks/${id}/reacquire`,
        trackId,
        error,
      );
      Alert.alert(
        'Re-acquire failed',
        `Could not start a re-acquisition. Your current audio is unchanged. ${RETRY_TAIL}`,
      );
    },
  });
}
