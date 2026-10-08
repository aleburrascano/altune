import { useQuery } from '@tanstack/react-query';

import type { DiscoverySearchHistoryResponse } from '@shared/api-client/discovery';

import { discoveryKeys } from '@shared/lib/query-keys';
import { useReportQueryFailure } from '@shared/telemetry/useReportQueryFailure';
import {
  useDiscoverFetchEnabled,
  useGatedDiscoverCall,
} from '@features/discover/hooks/useDiscoverFetchGate';
import { useDiscoverPort } from '@features/discover/ports/DiscoverPortProvider';

export function useSearchHistory() {
  const port = useDiscoverPort();
  const isHistoryEnabled = useDiscoverFetchEnabled();

  const {
    data: response,
    error,
    refetch: refetchHistory,
  } = useQuery<DiscoverySearchHistoryResponse>({
    queryKey: discoveryKeys.history,
    queryFn: () => port.listSearchHistory({ limit: 10 }),
    enabled: isHistoryEnabled,
  });

  useReportQueryFailure(error, 'history');

  const refetch = useGatedDiscoverCall(refetchHistory);

  return { data: response, error, refetch };
}
