/**
 * useDiscoverSearch — paged React Query wrapper for /v1/discovery/search.
 *
 * The server builds the whole ranked slate and pages it last (the result cache
 * holds the full pre-limit list), so page 2 is a slice of the same cached list
 * page 1 came from — consistent ordering, no provider re-fetch, and cheap.
 *
 * Callers still see ONE object shaped like a single response: pages are
 * flattened into `results`, and the metadata (search_id, corrections, related)
 * comes from page 1, which is the page that describes the search. Nothing
 * downstream had to learn about paging.
 */

import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query';

import {
  searchDiscovery,
  type DiscoverySearchResponse,
} from '@shared/api-client/discovery';

import { discoveryKeys } from '@shared/lib/query-keys';

/** One page. Matches the server's default so page boundaries are predictable. */
export const SEARCH_PAGE_SIZE = 20;

export function useDiscoverSearch(query: string, saveHistory: boolean = true) {
  const trimmed = query.trim();
  const queryClient = useQueryClient();

  const infinite = useInfiniteQuery({
    queryKey: discoveryKeys.search(trimmed),
    initialPageParam: 0,
    queryFn: ({ pageParam, signal }) => {
      // Abort any superseded as-you-type searches still in flight. Without this,
      // a slow search outlives the 300ms debounce, so fast typing leaves several
      // full searches running server-side at once — wasted work that also drains
      // provider rate-limit budgets (iTunes/MB then time out). The aborted fetch
      // cancels the request, which cancels the server's request context.
      void queryClient.cancelQueries({
        queryKey: discoveryKeys.searchPrefix,
        predicate: (q) => q.queryKey[2] !== trimmed,
      });
      return searchDiscovery(
        {
          q: trimmed,
          limit: SEARCH_PAGE_SIZE,
          offset: pageParam,
          // Only the first page IS the search. Later pages must not re-log it —
          // the server enforces this too; saying it here keeps the intent local.
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

  // One flattened response, so every consumer (the view state machine, the
  // impression logger, click telemetry) keeps working against the shape it
  // already knows.
  const data: DiscoverySearchResponse | undefined =
    first === undefined ? undefined : { ...first, results: pages.flatMap((p) => p.results) };

  return { ...infinite, data };
}
