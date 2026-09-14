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
