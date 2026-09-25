import { useCallback, useState } from 'react';
import { useQueryClient, type InfiniteData, type QueryClient } from '@tanstack/react-query';

import type { DiscoverySearchResponse } from '@shared/api-client/discovery';

type SearchPageParam = { offset: number; searchId: string | undefined };
type SearchPages = InfiniteData<DiscoverySearchResponse, SearchPageParam>;
type RefetchResult = { isError: boolean };
type Refetch = () => Promise<RefetchResult>;

function firstPageOnly(old: SearchPages | undefined): SearchPages | undefined {
  if (old === undefined) return old;
  return { pages: old.pages.slice(0, 1), pageParams: old.pageParams.slice(0, 1) };
}

function isInFlight(queryClient: QueryClient, queryKey: readonly unknown[]): boolean {
  return queryClient.isFetching({ queryKey, exact: true }) > 0;
}

type RefreshDeps = {
  queryClient: QueryClient;
  queryKey: readonly unknown[];
  refetch: Refetch;
  setHeld: (held: SearchPages | undefined) => void;
  setFailedKey: (key: readonly unknown[] | null) => void;
};

function snapshotAndTrim(queryClient: QueryClient, queryKey: readonly unknown[]) {
  const snapshot = queryClient.getQueryData<SearchPages>(queryKey);
  queryClient.setQueryData<SearchPages>(queryKey, firstPageOnly);
  return snapshot;
}

async function refetchFailed(deps: RefreshDeps, snapshot: SearchPages | undefined) {
  const outcome = await deps.refetch();
  const failed = outcome.isError && snapshot !== undefined;
  if (failed) deps.queryClient.setQueryData<SearchPages>(deps.queryKey, snapshot);
  return failed;
}

async function refreshOnce(deps: RefreshDeps): Promise<void> {
  if (isInFlight(deps.queryClient, deps.queryKey)) return;
  const snapshot = snapshotAndTrim(deps.queryClient, deps.queryKey);
  deps.setHeld(snapshot);
  const failed = await refetchFailed(deps, snapshot);
  deps.setFailedKey(failed ? deps.queryKey : null);
  deps.setHeld(undefined);
}

type Setters = Pick<RefreshDeps, 'setHeld' | 'setFailedKey'>;

function useRefreshCallback(queryKey: readonly unknown[], refetch: Refetch, setters: Setters) {
  const queryClient = useQueryClient();
  const { setHeld, setFailedKey } = setters;
  return useCallback(
    () => refreshOnce({ queryClient, queryKey, refetch, setHeld, setFailedKey }),
    [queryClient, queryKey, refetch, setHeld, setFailedKey],
  );
}

export function useRefreshFromFirstPage(queryKey: readonly unknown[], refetch: Refetch) {
  const [held, setHeld] = useState<SearchPages | undefined>(undefined);
  const [failedKey, setFailedKey] = useState<readonly unknown[] | null>(null);
  const refresh = useRefreshCallback(queryKey, refetch, { setHeld, setFailedKey });
  return { refresh, held, refreshFailed: failedKey === queryKey };
}
