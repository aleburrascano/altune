/**
 * useDiscoverSearch — React Query wrapper for /v1/discovery/search.
 *
 * Slice 45. Submit-only v1: query only fires when `query` is non-empty.
 */

import { useQuery, useQueryClient } from '@tanstack/react-query';

import {
  searchDiscovery,
  type DiscoverySearchResponse,
} from '../../../shared/api-client/discovery';

export function useDiscoverSearch(query: string, saveHistory: boolean = true) {
  const trimmed = query.trim();
  const queryClient = useQueryClient();
  return useQuery<DiscoverySearchResponse>({
    queryKey: ['discovery', 'search', trimmed],
    queryFn: ({ signal }) => {
      // Abort any superseded as-you-type searches still in flight. Without this,
      // a slow search outlives the 300ms debounce, so fast typing leaves several
      // full searches running server-side at once — wasted work that also drains
      // provider rate-limit budgets (iTunes/MB then time out). The aborted fetch
      // cancels the request, which cancels the server's request context.
      void queryClient.cancelQueries({
        queryKey: ['discovery', 'search'],
        predicate: (q) => q.queryKey[2] !== trimmed,
      });
      return searchDiscovery({ q: trimmed, saveHistory }, signal);
    },
    enabled: trimmed.length > 0,
  });
}
