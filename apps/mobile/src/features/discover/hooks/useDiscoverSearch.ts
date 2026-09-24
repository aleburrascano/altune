import { useCallback, useMemo } from 'react';
import { useInfiniteQuery, type InfiniteData, useQueryClient } from '@tanstack/react-query';

import {
  searchDiscovery,
  type DiscoveryProviderInfo,
  type DiscoverySearchResponse,
} from '@shared/api-client/discovery';

import { discoveryKeys } from '@shared/lib/query-keys';
import { useReportQueryFailure } from '@shared/telemetry/useReportQueryFailure';
import { useDiscoverFetchEnabled, useGatedDiscoverCall } from './discoverFetchGate';

export const SEARCH_PAGE_SIZE = 20;

// Every gate that turns typed text into a query — the debounced commit, the
// suggest fetch, the suggestion dropdown, the pending state — reads this one
// value, so they open on the same keystroke and cannot drift apart.
export const MIN_QUERY_LENGTH = 2;

// An endlessly `has_more` provider would otherwise grow one query's retained (and
// re-merged) page list without bound. Relevance is long gone 500 results in, so at
// the cap the query stops asking rather than dropping pages the user scrolled past.
export const MAX_SEARCH_PAGES = 25;

const noPageToFetch = (): Promise<void> => Promise.resolve();

export function useDiscoverSearch(
  query: string,
  /** Callers owe this: only an explicit submit or suggestion pick counts toward search history. */
  saveHistory: boolean = true,
) {
  const trimmed = query.trim();
  const queryClient = useQueryClient();
  const isSearchEnabled = useDiscoverFetchEnabled();

  const queryKey = useMemo(
    () => [...discoveryKeys.search(trimmed), saveHistory],
    [trimmed, saveHistory],
  );
  const {
    data: infiniteData,
    isLoading,
    isRefetching,
    error,
    isFetchNextPageError,
    refetch,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useInfiniteQuery({
    queryKey,
    initialPageParam: 0,
    queryFn: ({ pageParam, signal }) => {
      void queryClient.cancelQueries({
        queryKey: discoveryKeys.searchPrefix,
        predicate: (q) => q.queryKey[2] !== trimmed,
      });
      return searchDiscovery(
        {
          q: trimmed,
          limit: SEARCH_PAGE_SIZE,
          offset: pageParam,
          saveHistory: pageParam === 0 ? saveHistory : false,
        },
        signal,
      );
    },
    getNextPageParam: (lastPage, pages) => {
      if (pages.length >= MAX_SEARCH_PAGES) return undefined;
      return lastPage.has_more ? lastPage.offset + lastPage.results.length : undefined;
    },
    enabled: trimmed.length > 0 && isSearchEnabled,
  });

  useReportQueryFailure(error, 'search');

  const pages = infiniteData?.pages;
  const data = useMemo(() => mergePages(pages), [pages]);
  // react-query's refetch and fetchNextPage fetch whatever `enabled` says, so retry, pull to
  // refresh and the infinite scroll go through the switch themselves.
  const refetchFromFirstPage = useCallback(() => {
    queryClient.setQueryData<InfiniteData<DiscoverySearchResponse, number>>(queryKey, (old) =>
      old === undefined
        ? old
        : { pages: old.pages.slice(0, 1), pageParams: old.pageParams.slice(0, 1) },
    );
    return refetch();
  }, [queryClient, queryKey, refetch]);
  const retrySearch = useGatedDiscoverCall(refetchFromFirstPage);

  return {
    data,
    isLoading,
    isRefreshing: isRefetching && !isFetchingNextPage,
    error,
    /** The operator switched discovery off, so no query of ours will run. */
    isUnavailable: !isSearchEnabled,
    refetch: retrySearch,
    fetchNextPage: isSearchEnabled ? fetchNextPage : noPageToFetch,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
  };
}

function mergePages(pages: DiscoverySearchResponse[] = []): DiscoverySearchResponse | undefined {
  const [first] = pages;
  if (first === undefined) return undefined;
  return {
    ...first,
    results: pages.flatMap((page) => page.results),
    // Any degraded page leaves the merged list incomplete, not just the first.
    partial: pages.some((page) => page.partial),
    providers: mergeProviders(pages),
  };
}

function mergeProviders(pages: DiscoverySearchResponse[]): DiscoveryProviderInfo[] {
  const byProvider = new Map<string, DiscoveryProviderInfo>();
  for (const page of pages) {
    for (const info of page.providers) {
      const earlier = byProvider.get(info.provider);
      byProvider.set(info.provider, earlier === undefined ? info : acrossPages(earlier, info));
    }
  }
  return [...byProvider.values()];
}

// A provider that degraded on any page degraded the merged results, the rule the
// merged `partial` flag already follows; the counts describe every fetched page.
function acrossPages(
  earlier: DiscoveryProviderInfo,
  later: DiscoveryProviderInfo,
): DiscoveryProviderInfo {
  return {
    provider: earlier.provider,
    status: earlier.status === 'ok' ? later.status : earlier.status,
    result_count: earlier.result_count + later.result_count,
    latency_ms: Math.max(earlier.latency_ms, later.latency_ms),
  };
}
