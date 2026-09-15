import { useQueryClient } from '@tanstack/react-query';

import { failFastOnSettledContentFailure } from '../content-status';

/**
 * The `retry` option for a detail content-fetch query: the client's default
 * retry policy, except that a settled content failure (discovery.provider_* or
 * discovery.content_unserved) fails fast so the retry UI shows immediately.
 */
export function useContentFetchRetry(): (failureCount: number, error: Error) => boolean {
  const fallback = useQueryClient().getDefaultOptions().queries?.retry;
  return failFastOnSettledContentFailure(fallback);
}
