import { equalJitterMs, isRetryable } from '../errors';

export const RETRY_BACKOFF_BASE_MS = 1_000;
export const RETRY_BACKOFF_CAP_MS = 30_000;

export function retryDelayMs(failureCount: number, random: number): number {
  const exponent = Math.min(Math.max(failureCount, 0), 30);
  return equalJitterMs(RETRY_BACKOFF_BASE_MS, RETRY_BACKOFF_CAP_MS, exponent, random);
}

export const transientRetryOptions = {
  retry: (failureCount: number, error: unknown) => isRetryable(error) && failureCount < 5,
  retryDelay: (failureCount: number) => retryDelayMs(failureCount, Math.random()),
};
