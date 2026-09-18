import { useQuery } from '@tanstack/react-query';

import {
  listSearchHistory,
  type DiscoverySearchHistoryResponse,
} from '@shared/api-client/discovery';

import { discoveryKeys } from '@shared/lib/query-keys';
import { useReportQueryFailure } from '@shared/telemetry/useReportQueryFailure';
import { useDiscoverFetchEnabled } from './discoverFetchGate';

export function useSearchHistory() {
  const isHistoryEnabled = useDiscoverFetchEnabled();

  const { data, error } = useQuery<DiscoverySearchHistoryResponse>({
    queryKey: discoveryKeys.history,
    queryFn: () => listSearchHistory({ limit: 10 }),
    enabled: isHistoryEnabled,
  });

  useReportQueryFailure(error, 'history');

  return { data, error };
}
