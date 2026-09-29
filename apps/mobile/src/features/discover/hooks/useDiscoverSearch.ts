import { useMemo } from 'react';
import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query';

import {
  searchDiscovery,
  type DiscoveryProviderInfo,
  type DiscoverySearchResponse,
} from '@shared/api-client/discovery';

import { discoveryKeys, isSearchKeyFor } from '@shared/lib/query-keys';
import { useReportQueryFailure } from '@shared/telemetry/useReportQueryFailure';
import { useDiscoverFetchEnabled, useGatedDiscoverCall } from './discoverFetchGate';
import { useRefreshFromFirstPage } from './useRefreshFromFirstPage';
import { useRestartOnExpiredSlate } from './useRestartOnExpiredSlate';
import { MAX_SEARCH_PAGES, SEARCH_PAGE_SIZE } from '../searchLimits';

const noPageToFetch = (): Promise<void> => Promise.resolve();

type SearchPageParam = { offset: number; searchId: string | undefined };

const firstPageParam: SearchPageParam = { offset: 0, searchId: undefined };

function searchPageRequest(trimmed: string, saveHistory: boolean, pageParam: SearchPageParam) {
  return {
    q: trimmed,
    limit: SEARCH_PAGE_SIZE,
    offset: pageParam.offset,
    saveHistory: pageParam.offset === 0 ? saveHistory : false,
    ...(pageParam.searchId !== undefined ? { searchId: pageParam.searchId } : {}),
  };
}

function cancelStaleSearches(queryClient: ReturnType<typeof useQueryClient>, trimmed: string) {
  void queryClient.cancelQueries({
    queryKey: discoveryKeys.searchPrefix,
    predicate: (q) => !isSearchKeyFor(q.queryKey, trimmed),
  });
}

function searchQueryFn(
  queryClient: ReturnType<typeof useQueryClient>,
  trimmed: string,
  saveHistory: boolean,
) {
  return ({ pageParam, signal }: { pageParam: SearchPageParam; signal: AbortSignal }) => {
    cancelStaleSearches(queryClient, trimmed);
    return searchDiscovery(searchPageRequest(trimmed, saveHistory, pageParam), signal);
  };
}

function nextSearchPageParam(
  lastPage: DiscoverySearchResponse,
  pages: DiscoverySearchResponse[],
): SearchPageParam | undefined {
  if (pages.length >= MAX_SEARCH_PAGES) return undefined;
  if (!lastPage.has_more) return undefined;
  return { offset: lastPage.offset + lastPage.results.length, searchId: pages[0]?.search_id };
}

const SEARCH_RETRY_COUNT = 1;

type SearchQueryArgs = {
  queryKey: readonly unknown[];
  queryClient: ReturnType<typeof useQueryClient>;
  trimmed: string;
  saveHistory: boolean;
  isSearchEnabled: boolean;
};

function useSearchInfiniteQuery(args: SearchQueryArgs) {
  return useInfiniteQuery({
    queryKey: args.queryKey,
    initialPageParam: firstPageParam,
    queryFn: searchQueryFn(args.queryClient, args.trimmed, args.saveHistory),
    getNextPageParam: nextSearchPageParam,
    enabled: args.trimmed.length > 0 && args.isSearchEnabled,
    retry: SEARCH_RETRY_COUNT,
  });
}

export function useDiscoverSearch(query: string, saveHistory: boolean = true) {
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
  } = useSearchInfiniteQuery({ queryKey, queryClient, trimmed, saveHistory, isSearchEnabled });

  useReportQueryFailure(error, 'search');

  const { refresh, held, refreshFailed } = useRefreshFromFirstPage(queryKey, refetch);
  const pages = useRestartOnExpiredSlate(held ?? infiniteData, refresh);
  const data = useMemo(() => mergePages(pages), [pages]);
  const retrySearch = useGatedDiscoverCall(refresh);

  return {
    data,
    isLoading,
    isRefreshing: isRefetching && !isFetchingNextPage,
    error,
    isUnavailable: !isSearchEnabled,
    refetch: retrySearch,
    fetchNextPage: isSearchEnabled ? fetchNextPage : noPageToFetch,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    refreshFailed,
  };
}

function mergePages(pages: DiscoverySearchResponse[] = []): DiscoverySearchResponse | undefined {
  const [first] = pages;
  if (first === undefined) return undefined;
  return {
    ...first,
    results: pages.flatMap((page) => page.results),
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
