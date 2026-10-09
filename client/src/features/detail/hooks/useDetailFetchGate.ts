import { useQueryClient } from '@tanstack/react-query';

import { useLoopEnabled, useGatedCallback } from '@shared/killSwitch/killSwitchGate';

import { failFastOnSettledContentFailure } from '@/features/detail/domain/contentStatus';

export function useDetailFetchEnabled(): boolean {
  return useLoopEnabled('detailEnrichment');
}

export function useGatedRefetch(refetch: () => unknown): () => void {
  return useGatedCallback('detailEnrichment', refetch);
}

export function useContentFetchRetry(): (failureCount: number, error: Error) => boolean {
  const fallback = useQueryClient().getDefaultOptions().queries?.retry;
  return failFastOnSettledContentFailure(fallback);
}
