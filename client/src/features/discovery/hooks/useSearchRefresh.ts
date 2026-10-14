import { useCallback, useEffect, useMemo, useState } from 'react';
import { useQueryClient, type QueryClient, type QueryKey } from '@tanstack/react-query';

import type { DiscoverySearchResponse } from '@shared/apiClient/discovery';

import { useDiscoverFetchEnabled } from '@features/discovery/hooks/useDiscoverFetchGate';
import type { SearchPageParam, SearchPages } from '@features/discovery/domain/searchPaging';

type HeldSlate = { key: QueryKey; pages: SearchPages | undefined };
type RefetchResult = { isError: boolean };
type Refetch = () => Promise<RefetchResult>;

function firstPageOnly(old: SearchPages | undefined): SearchPages | undefined {
  if (old === undefined) return old;
  return { pages: old.pages.slice(0, 1), pageParams: old.pageParams.slice(0, 1) };
}

function isInFlight(queryClient: QueryClient, queryKey: QueryKey): boolean {
  return queryClient.isFetching({ queryKey, exact: true }) > 0;
}

type RefreshDeps = {
  queryClient: QueryClient;
  queryKey: QueryKey;
  refetch: Refetch;
  setHeld: (held: HeldSlate | undefined) => void;
  setFailedKey: (key: QueryKey | null) => void;
};

function snapshotAndTrim(queryClient: QueryClient, queryKey: QueryKey) {
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
  deps.setHeld({ key: deps.queryKey, pages: snapshot });
  try {
    const failed = await refetchFailed(deps, snapshot);
    deps.setFailedKey(failed ? deps.queryKey : null);
  } finally {
    deps.setHeld(undefined);
  }
}

type Setters = Pick<RefreshDeps, 'setHeld' | 'setFailedKey'>;

function useRefreshCallback(queryKey: QueryKey, refetch: Refetch, setters: Setters) {
  const queryClient = useQueryClient();
  const { setHeld, setFailedKey } = setters;
  return useCallback(
    () => refreshOnce({ queryClient, queryKey, refetch, setHeld, setFailedKey }),
    [queryClient, queryKey, refetch, setHeld, setFailedKey],
  );
}

export function useRefreshFromFirstPage(queryKey: QueryKey, refetch: Refetch) {
  const [held, setHeld] = useState<HeldSlate | undefined>(undefined);
  const [failedKey, setFailedKey] = useState<QueryKey | null>(null);
  const refresh = useRefreshCallback(queryKey, refetch, { setHeld, setFailedKey });
  return {
    refresh,
    held: held?.key === queryKey ? held.pages : undefined,
    refreshFailed: failedKey === queryKey,
  };
}

function slateMismatch(
  page: DiscoverySearchResponse,
  pageParam: SearchPageParam | undefined,
): boolean {
  const sentId = pageParam?.searchId;
  return sentId !== undefined && page.search_id !== sentId;
}

function firstSlateMismatch(source: SearchPages | undefined): number | undefined {
  if (source === undefined) return undefined;
  const index = source.pages.findIndex((page, i) => slateMismatch(page, source.pageParams[i]));
  return index === -1 ? undefined : index;
}

function useStableGatedRestart(restart: () => Promise<void>): () => void {
  const isEnabled = useDiscoverFetchEnabled();
  return useCallback(() => {
    if (isEnabled) void restart();
  }, [isEnabled, restart]);
}

function useRestartWhenExpired(expiredAt: number | undefined, restart: () => void): void {
  useEffect(() => {
    if (expiredAt !== undefined) restart();
  }, [expiredAt, restart]);
}

export function useRestartOnExpiredSlate(
  source: SearchPages | undefined,
  restart: () => Promise<void>,
): DiscoverySearchResponse[] | undefined {
  const expiredAt = firstSlateMismatch(source);
  useRestartWhenExpired(expiredAt, useStableGatedRestart(restart));
  return useMemo(
    () => source?.pages.slice(0, expiredAt ?? source.pages.length),
    [source, expiredAt],
  );
}
