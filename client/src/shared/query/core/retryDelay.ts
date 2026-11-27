import { clampedExponent, equalJitterMs } from '@shared/timing';
import { ApiError, isRetryable } from '@shared/failure';

const RETRY_BACKOFF_BASE_MS = 1_000;
const RETRY_BACKOFF_CAP_MS = 30_000;

function retryDelayMs(failureCount: number, random: number): number {
  const exponent = clampedExponent(failureCount, 0);
  return equalJitterMs(RETRY_BACKOFF_BASE_MS, RETRY_BACKOFF_CAP_MS, exponent, random);
}

const SERVER_ASKED_FLOOR_MS = 1_000;
const SERVER_ASKED_SPREAD = 0.25;

function serverAskedDelayMs(error: unknown, random: number): number | undefined {
  if (!(error instanceof ApiError) || error.retryAfterMs === undefined) return undefined;
  const asked = Math.min(Math.max(error.retryAfterMs, SERVER_ASKED_FLOOR_MS), RETRY_BACKOFF_CAP_MS);
  return asked + Math.floor(random * asked * SERVER_ASKED_SPREAD);
}

export interface RetryClock {
  random: () => number;
}

export function createTransientRetryOptions({ random }: RetryClock) {
  return {
    retry: (failureCount: number, error: unknown) => isRetryable(error) && failureCount < 5,
    retryDelay: (failureCount: number, error: unknown) =>
      serverAskedDelayMs(error, random()) ?? retryDelayMs(failureCount, random()),
  };
}

export const transientRetryOptions = createTransientRetryOptions({ random: () => Math.random() });
