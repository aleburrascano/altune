import type { DefaultOptions } from '@tanstack/react-query';

import { ApiError, type ApiErrorCode } from '@shared/failure';
import type { DiscoveryProviderStatus } from '@shared/catalog';

export const DETAIL_LIST_CAP = 100;

export const DETAIL_CONTENT_STALE_MS = 30 * 60 * 1000;

function isContentError(
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

const SETTLED_CONTENT_CODES: ReadonlySet<ApiErrorCode> = new Set<ApiErrorCode>([
  'discovery.content_unserved',
  'discovery.provider_timeout',
  'discovery.provider_rate_limited',
  'discovery.provider_circuit_open',
  'discovery.provider_error',
]);

function isSettledContentFailure(error: unknown): boolean {
  if (!(error instanceof ApiError) || error.code === undefined) return false;
  return SETTLED_CONTENT_CODES.has(error.code);
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
