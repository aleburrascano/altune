import type { DefaultOptions } from '@tanstack/react-query';

import { ApiError } from '@shared/errors';
import type { DiscoveryProviderStatus } from '@shared/api-client/discovery';

export const DETAIL_LIST_CAP = 100;

export const DETAIL_CONTENT_STALE_MS = 30 * 60 * 1000;

export function isContentError(
  queryFailed: boolean,
  response: { status: DiscoveryProviderStatus } | null | undefined,
): boolean {
  return queryFailed || hasDegradedStatus(response);
}

export function hasDegradedStatus(
  response: { status: DiscoveryProviderStatus } | null | undefined,
): boolean {
  return response != null && response.status !== 'ok';
}

const CONTENT_UNSERVED_CODE = 'discovery.content_unserved';
const PROVIDER_FAILURE_CODE_PREFIX = 'discovery.provider_';

export function isSettledContentFailure(error: unknown): boolean {
  if (!(error instanceof ApiError) || error.code === undefined) return false;
  return (
    error.code === CONTENT_UNSERVED_CODE || error.code.startsWith(PROVIDER_FAILURE_CODE_PREFIX)
  );
}

export type ContentFailure = 'settled' | 'transient';

export function contentFailure(
  queryFailed: boolean,
  error: unknown,
  response: { status: DiscoveryProviderStatus } | null | undefined,
): ContentFailure | null {
  if (!isContentError(queryFailed, response)) return null;
  return isSettledContentFailure(error) ? 'settled' : 'transient';
}

type RetryValue = NonNullable<DefaultOptions['queries']>['retry'];

function shouldRetry(retry: RetryValue, failureCount: number, error: Error): boolean {
  const value = retry ?? 3;
  if (typeof value === 'function') return value(failureCount, error);
  if (typeof value === 'number') return failureCount < value;
  return value;
}

export function failFastOnSettledContentFailure(
  fallback: RetryValue,
): (failureCount: number, error: Error) => boolean {
  return (failureCount, error) =>
    !isSettledContentFailure(error) && shouldRetry(fallback, failureCount, error);
}
