import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query';

import { searchDiscovery, type DiscoverySearchResponse } from '@shared/api-client/discovery';

import { discoveryKeys } from '@shared/lib/query-keys';
import { useReportQueryFailure } from '@shared/telemetry/useReportQueryFailure';

export const SEARCH_PAGE_SIZE = 20;

// Every gate that turns typed text into a query — the debounced commit, the
// suggest fetch, the suggestion dropdown, the pending state — reads this one
// value, so they open on the same keystroke and cannot drift apart.
export const MIN_QUERY_LENGTH = 2;

export function useDiscoverSearch(query: string, saveHistory: boolean = true) {
  const trimmed = query.trim();
  const queryClient = useQueryClient();

  const {
    data: infiniteData,
    isLoading,
    error,
    refetch,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useInfiniteQuery({
    queryKey: discoveryKeys.search(trimmed),
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
    getNextPageParam: (lastPage) =>
      lastPage.has_more ? lastPage.offset + lastPage.results.length : undefined,
    enabled: trimmed.length > 0,
  });

  useReportQueryFailure(error, 'search');

  const pages = infiniteData?.pages ?? [];
  const first = pages[0];

  const data: DiscoverySearchResponse | undefined =
    first === undefined
      ? undefined
      : {
          ...first,
          results: pages.flatMap((p) => p.results),
          // Any degraded page leaves the merged list incomplete, not just the first.
          partial: pages.some((p) => p.partial),
        };

  return { data, isLoading, error, refetch, fetchNextPage, hasNextPage, isFetchingNextPage };
}
