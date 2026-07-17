/**
 * useSearchHistory — React Query wrapper for /v1/discovery/search-history.
 *
 * Slice 45. Powers the empty-no-query state.
 */

import { useQuery } from '@tanstack/react-query';

import {
  listSearchHistory,
  type DiscoverySearchHistoryResponse,
} from '@shared/api-client/discovery';

import { discoveryKeys } from '../keys';

export function useSearchHistory() {
  return useQuery<DiscoverySearchHistoryResponse>({
    queryKey: discoveryKeys.history,
    queryFn: () => listSearchHistory({ limit: 10 }),
  });
}
