/**
 * useDiscoverSearch — React Query wrapper for /v1/discovery/search.
 *
 * Slice 45. Submit-only v1: query only fires when `query` is non-empty.
 */

import { useQuery } from '@tanstack/react-query';

import {
  searchDiscovery,
  type DiscoverySearchResponse,
} from '../../../shared/api-client/discovery';

export function useDiscoverSearch(query: string) {
  const trimmed = query.trim();
  return useQuery<DiscoverySearchResponse>({
    queryKey: ['discovery', 'search', trimmed],
    queryFn: () => searchDiscovery({ q: trimmed }),
    enabled: trimmed.length > 0,
  });
}
