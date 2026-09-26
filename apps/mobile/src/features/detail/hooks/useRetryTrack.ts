import { useMutation, type UseMutationResult } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import { retryAcquisition } from '@shared/api-client/tracks';
import {
  recordRetryRequest,
  recordRetryTapped,
  retryFailureOutcome,
  type RetryEntryPoint,
} from '@shared/acquisition/acquisitionTelemetry';
import { patchTrackStatus } from '@shared/acquisition/trackStatusStore';
import { guardedMutationOptions } from '@shared/session/signOutCleanup';

export type RetryTrack = Pick<UseMutationResult<void, unknown, TrackId>, 'mutate' | 'isPending'>;

function markPending(trackId: TrackId): Record<string, never> {
  patchTrackStatus(trackId, { acquisitionStatus: 'pending', failureMessage: null }, 'optimistic');
  return {};
}

function markFailed(entryPoint: RetryEntryPoint) {
  return (error: Error, trackId: TrackId): void => {
    recordRetryRequest(trackId, entryPoint, retryFailureOutcome(error));
    console.warn('[detail] retry track failed', { trackId, error: error.message });
    patchTrackStatus(trackId, { acquisitionStatus: 'failed', failureMessage: error.message });
  };
}

async function sendRetry(trackId: TrackId, entryPoint: RetryEntryPoint): Promise<void> {
  recordRetryRequest(trackId, entryPoint, { kind: 'sent' });
  await retryAcquisition(trackId);
  recordRetryRequest(trackId, entryPoint, { kind: 'succeeded' });
}

function useRetryMutation(entryPoint: RetryEntryPoint) {
  return useMutation(
    guardedMutationOptions({
      mutationFn: (trackId: TrackId) => sendRetry(trackId, entryPoint),
      onMutate: markPending,
      onError: markFailed(entryPoint),
    }),
  );
}

export function useRetryTrack(entryPoint: RetryEntryPoint): RetryTrack {
  const mutation = useRetryMutation(entryPoint);
  const mutate = (trackId: TrackId): void => {
    recordRetryTapped(trackId, entryPoint);
    mutation.mutate(trackId);
  };
  return { mutate, isPending: mutation.isPending };
}
