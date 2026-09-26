import { useMutation, type UseMutationResult } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import { retryAcquisition } from '@shared/api-client/tracks';
import { patchTrackStatus } from '@shared/acquisition/trackStatusStore';
import { guardedMutationOptions } from '@shared/session/signOutCleanup';

export type RetryTrack = Pick<UseMutationResult<void, unknown, TrackId>, 'mutate' | 'isPending'>;

function markPending(trackId: TrackId): Record<string, never> {
  patchTrackStatus(trackId, { acquisitionStatus: 'pending', failureMessage: null });
  return {};
}

function markFailed(error: Error, trackId: TrackId): void {
  console.warn('[detail] retry track failed', { trackId, error: error.message });
  patchTrackStatus(trackId, { acquisitionStatus: 'failed', failureMessage: error.message });
}

export function useRetryTrack(): RetryTrack {
  return useMutation(
    guardedMutationOptions({
      mutationFn: (trackId: TrackId) => retryAcquisition(trackId),
      onMutate: markPending,
      onError: markFailed,
    }),
  );
}
