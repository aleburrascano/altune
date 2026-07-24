import { useQuery } from '@tanstack/react-query';

import {
  listSearchHistory,
  type DiscoverySearchHistoryResponse,
} from '@shared/api-client/discovery';

import { discoveryKeys } from '@shared/lib/query-keys';

export function useSearchHistory() {
  return useQuery<DiscoverySearchHistoryResponse>({
    queryKey: discoveryKeys.history,
    queryFn: () => listSearchHistory({ limit: 10 }),
  });
}
