import { useQueryClient } from '@tanstack/react-query';

import { failFastOnSettledContentFailure } from '@features/detail/core/contentStatus';

export function useContentFetchRetry(): (failureCount: number, error: Error) => boolean {
  const fallback = useQueryClient().getDefaultOptions().queries?.retry;
  return failFastOnSettledContentFailure(fallback);
}
