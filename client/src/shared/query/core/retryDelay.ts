import { clampedExponent, equalJitterMs } from '@shared/timing';
import { ApiError, isRetryable } from '@shared/failure';

const RETRY_BACKOFF_BASE_MS = 1_000;
const RETRY_BACKOFF_CAP_MS = 30_000;

function retryDelayMs(failureCount: number, random: number): number {
  const exponent = clampedExponent(failureCount, 0);
  return equalJitterMs(RETRY_BACKOFF_BASE_MS, RETRY_BACKOFF_CAP_MS, exponent, random);
}

function serverAskedDelayMs(error: unknown): number | undefined {
  if (!(error instanceof ApiError) || error.retryAfterMs === undefined) return undefined;
  return Math.min(error.retryAfterMs, RETRY_BACKOFF_CAP_MS);
}

export interface RetryClock {
  random: () => number;
}

export function createTransientRetryOptions({ random }: RetryClock) {
  return {
    retry: (failureCount: number, error: unknown) => isRetryable(error) && failureCount < 5,
    retryDelay: (failureCount: number, error: unknown) =>
      serverAskedDelayMs(error) ?? retryDelayMs(failureCount, random()),
  };
}

export const transientRetryOptions = createTransientRetryOptions({ random: () => Math.random() });
