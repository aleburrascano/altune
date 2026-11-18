import { useQueryClient } from '@tanstack/react-query';

import { useLoopEnabled, useGatedCallback } from '@shared/killSwitch';

import { failFastOnSettledContentFailure } from '@features/detail/core/contentStatus';

export function useDetailFetchEnabled(): boolean {
  return useLoopEnabled('detailEnrichment');
}

export function useGatedRefetch(refetch: () => void | Promise<unknown>): () => void {
  return useGatedCallback('detailEnrichment', refetch);
}

export function useContentFetchRetry(): (failureCount: number, error: Error) => boolean {
  const fallback = useQueryClient().getDefaultOptions().queries?.retry;
  return failFastOnSettledContentFailure(fallback);
}
