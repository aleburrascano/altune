import type { DefaultOptions } from '@tanstack/react-query';

import { ApiError } from '@shared/errors';
import type { DiscoveryProviderStatus } from '@shared/api-client/discovery';

/**
 * The bound every detail list asks the server for, so an oversized collection
 * (a box set's tracklist, a prolific artist's discography) cannot pull an
 * unbounded payload into one screen. One owner, because the lists are read side
 * by side and a per-list value drifts.
 */
export const DETAIL_LIST_CAP = 100;

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
  return queryFailed || hasDegradedStatus(response);
}

/** A response that arrived carrying a non-'ok' provider status. Absent is not degraded. */
export function hasDegradedStatus(
  response: { status: DiscoveryProviderStatus } | null | undefined,
): boolean {
  return response != null && response.status !== 'ok';
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

/**
 * Why a content list failed, in the only terms its retry affordance cares
 * about: a settled failure is one the server has already decided, so asking
 * again returns the same answer, while a transient one can succeed on a second
 * ask.
 */
export type ContentFailure = 'settled' | 'transient';

/**
 * The classification callers render from. A degraded provider status arrives on
 * a 200 and stays transient: the request succeeded, only the provider behind it
 * was briefly unhealthy.
 */
export function contentFailure(
  queryFailed: boolean,
  error: unknown,
  response: { status: DiscoveryProviderStatus } | null | undefined,
): ContentFailure | null {
  if (!isContentError(queryFailed, response)) return null;
  return isSettledContentFailure(error) ? 'settled' : 'transient';
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
