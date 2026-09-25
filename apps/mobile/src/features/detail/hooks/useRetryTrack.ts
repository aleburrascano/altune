import { useMutation, type UseMutationResult } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import { retryAcquisition } from '@shared/api-client/tracks';
import { patchTrackStatus } from '@shared/acquisition/trackStatusStore';

export type RetryTrack = Pick<UseMutationResult<void, Error, TrackId>, 'mutate' | 'isPending'>;

function markPending(trackId: TrackId): void {
  patchTrackStatus(trackId, { acquisitionStatus: 'pending', failureMessage: null });
}

function markFailed(error: Error, trackId: TrackId): void {
  console.warn('[detail] retry track failed', { trackId, error: error.message });
  patchTrackStatus(trackId, { acquisitionStatus: 'failed', failureMessage: error.message });
}

export function useRetryTrack(): RetryTrack {
  return useMutation<void, Error, TrackId>({
    mutationFn: (trackId) => retryAcquisition(trackId),
    onMutate: markPending,
    onError: markFailed,
  });
}
