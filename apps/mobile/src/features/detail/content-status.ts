import type { DefaultOptions } from '@tanstack/react-query';

import { ApiError } from '@shared/api-client/errors';
import type { DiscoveryProviderStatus } from '@shared/api-client/discovery';

/**
 * The one reading of a content fetch's health for detail lists. A thrown query
 * is an error, and so is any non-'ok' provider status (timeout, rate_limited,
 * circuit_open, error): those are transient outages that must surface the retry
 * UI, never a false "nothing found" empty state. A response that has not
 * arrived yet (loading or disabled) is not an error.
 */
export function isContentError(
  queryFailed: boolean,
  response: { status: DiscoveryProviderStatus } | null | undefined,
): boolean {
  return queryFailed || (response != null && response.status !== 'ok');
}

const CONTENT_UNSERVED_CODE = 'discovery.content_unserved';
const PROVIDER_FAILURE_CODE_PREFIX = 'discovery.provider_';

/**
 * A content fetch the server has already settled as failed: the provider timed
 * out, was rate limited, had its circuit open or errored, or the content is
 * unserved. Since #1102 these arrive as 404/5xx carrying a `discovery.*` code
 * instead of HTTP 200 with a non-'ok' status.
 */
export function isSettledContentFailure(error: unknown): boolean {
  if (!(error instanceof ApiError) || error.code === undefined) return false;
  return (
    error.code === CONTENT_UNSERVED_CODE || error.code.startsWith(PROVIDER_FAILURE_CODE_PREFIX)
  );
}

type RetryValue = NonNullable<DefaultOptions['queries']>['retry'];

// react-query's own reading of a retry option (query-core retryer; 3 on a client).
function shouldRetry(retry: RetryValue, failureCount: number, error: Error): boolean {
  const value = retry ?? 3;
  if (typeof value === 'function') return value(failureCount, error);
  if (typeof value === 'number') return failureCount < value;
  return value;
}

/**
 * Query retry for detail content fetches. A settled content failure is never
 * retried, so the list shows its retry UI at once, as it did when the failure
 * came back as a 200. An automatic re-ask cannot help: on a timeout the server
 * already spent its provider deadline, retrying a rate limit or open circuit
 * only prolongs it, and unserved content stays unserved; the user can still tap
 * retry. Every other failure defers to `fallback`, the client's default policy.
 */
export function failFastOnSettledContentFailure(
  fallback: RetryValue,
): (failureCount: number, error: Error) => boolean {
  return (failureCount, error) =>
    !isSettledContentFailure(error) && shouldRetry(fallback, failureCount, error);
}
