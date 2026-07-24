import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query';

import { searchDiscovery, type DiscoverySearchResponse } from '@shared/api-client/discovery';

import { discoveryKeys } from '@shared/lib/query-keys';

export const SEARCH_PAGE_SIZE = 20;

export function useDiscoverSearch(query: string, saveHistory: boolean = true) {
  const trimmed = query.trim();
  const queryClient = useQueryClient();

  const infinite = useInfiniteQuery({
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

  const pages = infinite.data?.pages ?? [];
  const first = pages[0];

  const data: DiscoverySearchResponse | undefined =
    first === undefined ? undefined : { ...first, results: pages.flatMap((p) => p.results) };

  return { ...infinite, data };
}
