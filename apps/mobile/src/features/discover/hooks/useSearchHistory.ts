import { useQuery } from '@tanstack/react-query';

import {
  listSearchHistory,
  type DiscoverySearchHistoryResponse,
} from '@shared/api-client/discovery';

import { discoveryKeys } from '@shared/lib/query-keys';
import { useReportQueryFailure } from '@shared/telemetry/useReportQueryFailure';

export function useSearchHistory() {
  const { data, error } = useQuery<DiscoverySearchHistoryResponse>({
    queryKey: discoveryKeys.history,
    queryFn: () => listSearchHistory({ limit: 10 }),
  });

  useReportQueryFailure(error, 'history');

  return { data, error };
}
