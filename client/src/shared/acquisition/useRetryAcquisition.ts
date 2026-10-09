import type { UseMutationResult } from '@tanstack/react-query';

import { useAppMutation } from '@shared/query/useAppMutation';
import type { TrackId } from '@shared/api-client/ids';
import {
  recordRetryRequest,
  retryFailureOutcome,
  type RetryEntryPoint,
} from '@shared/acquisition/acquisitionTelemetry';
import { useAcquisitionPort } from '@shared/acquisition/ports/AcquisitionPortProvider';
import type { AcquisitionPort } from '@shared/acquisition/ports/acquisitionPort';
import { guardedMutationOptions } from '@shared/session/guardedMutation';

export type SharedRetryOptions<TContext extends object> = {
  action: string;
  entryPoint: RetryEntryPoint | undefined;
  mutationKey?: readonly unknown[];
  onMutate: (trackId: TrackId) => TContext;
  onError: (error: Error, trackId: TrackId, context: TContext) => void;
};

async function sendRetry(
  port: AcquisitionPort,
  trackId: TrackId,
  entryPoint: RetryEntryPoint | undefined,
) {
  recordRetryRequest(trackId, entryPoint, { kind: 'sent' });
  await port.retryAcquisition(trackId);
  recordRetryRequest(trackId, entryPoint, { kind: 'succeeded' });
}

type Fenced<TContext> = TContext & { epoch: number };

function recordedFailure<TContext extends object>(
  entryPoint: RetryEntryPoint | undefined,
  onError: SharedRetryOptions<TContext>['onError'],
) {
  return (error: Error, trackId: TrackId, context: Fenced<TContext>): void => {
    recordRetryRequest(trackId, entryPoint, retryFailureOutcome(error));
    onError(error, trackId, context);
  };
}

function retryOptions<TContext extends object>(
  port: AcquisitionPort,
  options: SharedRetryOptions<TContext>,
) {
  return guardedMutationOptions({
    mutationFn: (trackId: TrackId) => sendRetry(port, trackId, options.entryPoint),
    onMutate: options.onMutate,
    onError: recordedFailure(options.entryPoint, options.onError),
  });
}

export function useSharedRetryAcquisition<TContext extends object>(
  options: SharedRetryOptions<TContext>,
): UseMutationResult<void, Error, TrackId, Fenced<TContext>> {
  const port = useAcquisitionPort();
  return useAppMutation({
    ...retryOptions(port, options),
    ...(options.mutationKey ? { mutationKey: options.mutationKey } : {}),
    action: options.action,
    trackIdOf: (trackId: TrackId) => trackId,
  });
}
