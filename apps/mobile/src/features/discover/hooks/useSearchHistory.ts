import { useQuery } from '@tanstack/react-query';

import type { DiscoverySearchHistoryResponse } from '@shared/api-client/discovery';

import { discoveryKeys } from '@shared/lib/query-keys';
import { useReportQueryFailure } from '@shared/telemetry/useReportQueryFailure';
import { useDiscoverFetchEnabled } from '@features/discover/hooks/discoverFetchGate';
import { useDiscoverPort } from '@features/discover/ports/DiscoverPortProvider';

export function useSearchHistory() {
  const port = useDiscoverPort();
  const isHistoryEnabled = useDiscoverFetchEnabled();

  const {
    data: response,
    error,
    refetch,
  } = useQuery<DiscoverySearchHistoryResponse>({
    queryKey: discoveryKeys.history,
    queryFn: () => port.listSearchHistory({ limit: 10 }),
    enabled: isHistoryEnabled,
  });

  useReportQueryFailure(error, 'history');

  return { data: response, error, refetch };
}
