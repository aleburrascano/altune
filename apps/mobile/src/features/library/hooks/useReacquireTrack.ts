import { Alert } from 'react-native';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import { reacquireTrack } from '@shared/api-client/tracks';
import { patchTrackInCaches } from '@shared/events/trackCachePatch';

import { dropVanishedTrack } from './dropVanishedTrack';
import { logTrackMutationFailure } from './logTrackMutationFailure';
import { classifyLibraryError, failureTail } from '../state';

export function useReacquireTrack() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (trackId: TrackId) => reacquireTrack(trackId),
    onSuccess: (_data, trackId) => {
      patchTrackInCaches(queryClient, trackId, { acquisition_status: 'pending' });
    },
    onError: (error, trackId) => {
      const failure = classifyLibraryError(error);
      if (failure === 'not-found') return dropVanishedTrack(queryClient, trackId);
      logTrackMutationFailure(
        're-acquire track',
        (id) => `POST /v1/tracks/${id}/reacquire`,
        trackId,
        error,
      );
      Alert.alert(
        'Re-acquire failed',
        `Could not start a re-acquisition. Your current audio is unchanged. ${failureTail(failure)}`,
      );
    },
  });
}
