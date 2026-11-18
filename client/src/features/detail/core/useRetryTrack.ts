import { type UseMutationResult } from '@tanstack/react-query';
import type { TrackId } from '@shared/catalog';
import {
  recordRetryTapped,
  type RetryEntryPoint,
  failedTrackStatus,
  patchTrackStatus,
  useSharedRetryAcquisition,
} from '@shared/acquisition';
import type { DetailPort } from '@features/detail/ports/detailPort';
import { useDetailPort } from '@features/detail/view/DetailPortProvider';

export type RetryTrack = Pick<UseMutationResult<void, Error, TrackId>, 'mutate' | 'isPending'>;

type RetryMutateOptions = Parameters<RetryTrack['mutate']>[1];

function markPending(trackId: TrackId): Record<string, never> {
  patchTrackStatus(trackId, { acquisitionStatus: 'pending', failureMessage: null }, 'optimistic');
  return {};
}

function markFailed(port: DetailPort, error: Error, trackId: TrackId): void {
  port.warn('[detail] retry track failed', { trackId, error: error.message });
  patchTrackStatus(trackId, failedTrackStatus(error));
}

function useRetryMutation(entryPoint: RetryEntryPoint) {
  const port = useDetailPort();
  return useSharedRetryAcquisition({
    action: 'detail.retry',
    entryPoint,
    onMutate: markPending,
    onError: (error, trackId) => markFailed(port, error, trackId),
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
