import { type UseMutationResult } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import { recordRetryTapped, type RetryEntryPoint } from '@shared/acquisition/acquisitionTelemetry';
import { patchTrackStatus } from '@shared/acquisition/trackStatusStore';
import { useSharedRetryAcquisition } from '@shared/acquisition/useRetryAcquisition';

export type RetryTrack = Pick<UseMutationResult<void, unknown, TrackId>, 'mutate' | 'isPending'>;

type RetryMutateOptions = Parameters<RetryTrack['mutate']>[1];

function markPending(trackId: TrackId): Record<string, never> {
  patchTrackStatus(trackId, { acquisitionStatus: 'pending', failureMessage: null }, 'optimistic');
  return {};
}

function markFailed(error: Error, trackId: TrackId): void {
  console.warn('[detail] retry track failed', { trackId, error: error.message });
  patchTrackStatus(trackId, { acquisitionStatus: 'failed', failureMessage: error.message });
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
