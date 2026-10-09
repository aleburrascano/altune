import { useQuery } from '@tanstack/react-query';

import type { DiscoverySearchHistoryResponse } from '@shared/api-client/discovery';

import { isRetryable } from '@shared/errors';
import { discoveryKeys } from '@shared/query/queryKeys';
import { useClearSearchHistory as useSharedClearSearchHistory } from '@shared/search-history/useClearSearchHistory';
import { useReportQueryFailure } from '@shared/telemetry/useReportQueryFailure';
import {
  useDiscoverFetchEnabled,
  useGatedDiscoverCall,
} from '@features/discovery/hooks/useDiscoverFetchGate';
import { useDiscoverPort } from '@features/discovery/ports/DiscoverPortProvider';

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

type ClearSearchHistory = {
  clear: () => void;
  error: Error | null;
};

export function useDiscoverClearSearchHistory(): ClearSearchHistory {
  const clearHistoryMutation = useSharedClearSearchHistory({
    action: 'discover.clear_search_history',
    retryOptions: { retry: (failureCount, error) => isRetryable(error) && failureCount < 5 },
  });
  const { mutate, error } = clearHistoryMutation;
  const clear = useGatedDiscoverCall(() => mutate());
  useReportQueryFailure(error, 'clear_history');

  return { clear, error: error ?? null };
}
