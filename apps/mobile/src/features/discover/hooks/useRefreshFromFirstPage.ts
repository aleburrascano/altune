import { useCallback, useState } from 'react';
import { useQueryClient, type InfiniteData, type QueryClient } from '@tanstack/react-query';

import type { DiscoverySearchResponse } from '@shared/api-client/discovery';

type SearchPageParam = { offset: number; searchId: string | undefined };
type SearchPages = InfiniteData<DiscoverySearchResponse, SearchPageParam>;
type RefetchResult = { isError: boolean };

function firstPageOnly(old: SearchPages | undefined): SearchPages | undefined {
  if (old === undefined) return old;
  return { pages: old.pages.slice(0, 1), pageParams: old.pageParams.slice(0, 1) };
}

function isInFlight(queryClient: QueryClient, queryKey: readonly unknown[]): boolean {
  return queryClient.isFetching({ queryKey, exact: true }) > 0;
}

export function useRefreshFromFirstPage(
  queryKey: readonly unknown[],
  refetch: () => Promise<RefetchResult>,
) {
  const queryClient = useQueryClient();
  const [held, setHeld] = useState<SearchPages | undefined>(undefined);
  const [failedKey, setFailedKey] = useState<readonly unknown[] | null>(null);
  const refresh = useCallback(async (): Promise<void> => {
    if (isInFlight(queryClient, queryKey)) return;
    const snapshot = queryClient.getQueryData<SearchPages>(queryKey);
    setHeld(snapshot);
    queryClient.setQueryData<SearchPages>(queryKey, firstPageOnly);
    const result = await refetch();
    const failed = result.isError && snapshot !== undefined;
    if (failed) queryClient.setQueryData<SearchPages>(queryKey, snapshot);
    setFailedKey(failed ? queryKey : null);
    setHeld(undefined);
  }, [queryClient, queryKey, refetch]);
  return { refresh, held, refreshFailed: failedKey === queryKey };
}
