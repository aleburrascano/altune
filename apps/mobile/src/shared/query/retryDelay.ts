import { isRetryable } from '../api-client';

/** The first retry waits between half of this and this. */
export const RETRY_BACKOFF_BASE_MS = 1_000;
/** No retry ever waits longer than this, however many attempts have failed. */
export const RETRY_BACKOFF_CAP_MS = 30_000;

/**
 * Delay before the retry that follows failure number `failureCount` (0-based, as
 * react-query counts): exponential from RETRY_BACKOFF_BASE_MS, capped at
 * RETRY_BACKOFF_CAP_MS, with equal jitter so the wait lands in [ceiling/2, ceiling].
 * Without the jitter every client that started failing at the same moment — a shared
 * backend outage — would retry on the same schedule and arrive as one herd (#1662).
 * `random` is a sample in [0, 1).
 */
export function retryDelayMs(failureCount: number, random: number): number {
  const exponent = Math.min(Math.max(failureCount, 0), 30);
  const ceiling = Math.min(RETRY_BACKOFF_CAP_MS, RETRY_BACKOFF_BASE_MS * 2 ** exponent);
  return Math.round(ceiling / 2 + random * (ceiling / 2));
}

/**
 * The one transient-retry policy: retry only isRetryable() errors, at most 5 times, on the
 * jittered backoff above. Queries get it as the client default; a mutation opts in only when
 * repeating it is safe (#841).
 */
export const transientRetryOptions = {
  retry: (failureCount: number, error: unknown) => isRetryable(error) && failureCount < 5,
  retryDelay: (failureCount: number) => retryDelayMs(failureCount, Math.random()),
};
