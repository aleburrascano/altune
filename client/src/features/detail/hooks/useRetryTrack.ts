import { type UseMutationResult } from '@tanstack/react-query';

import type { TrackId } from '@shared/apiClient';
import {
  recordRetryTapped,
  type RetryEntryPoint,
  failedTrackStatus,
  patchTrackStatus,
  useSharedRetryAcquisition,
} from '@shared/acquisition';
export type RetryTrack = Pick<UseMutationResult<void, Error, TrackId>, 'mutate' | 'isPending'>;

type RetryMutateOptions = Parameters<RetryTrack['mutate']>[1];

function markPending(trackId: TrackId): Record<string, never> {
  patchTrackStatus(trackId, { acquisitionStatus: 'pending', failureMessage: null }, 'optimistic');
  return {};
}

function markFailed(error: Error, trackId: TrackId): void {
  console.warn('[detail] retry track failed', { trackId, error: error.message });
  patchTrackStatus(trackId, failedTrackStatus(error));
}

function useRetryMutation(entryPoint: RetryEntryPoint) {
  return useSharedRetryAcquisition({
    action: 'detail.retry',
    entryPoint,
    onMutate: markPending,
    onError: markFailed,
  });
}

export function useRetryTrack(entryPoint: RetryEntryPoint): RetryTrack {
  const mutation = useRetryMutation(entryPoint);
  const mutate = (trackId: TrackId, options?: RetryMutateOptions): void => {
    recordRetryTapped(trackId, entryPoint);
    mutation.mutate(trackId, options);
  };
  return { mutate, isPending: mutation.isPending };
}
