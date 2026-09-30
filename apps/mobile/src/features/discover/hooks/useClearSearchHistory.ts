import { isRetryable } from '@shared/api-client';
import { useClearSearchHistory as useSharedClearSearchHistory } from '@shared/search-history/useClearSearchHistory';
import { useReportQueryFailure } from '@shared/telemetry/useReportQueryFailure';
import { useGatedDiscoverCall } from './discoverFetchGate';

type ClearSearchHistory = {
  clear: () => void;
  error: Error | null;
};

export function useClearSearchHistory(): ClearSearchHistory {
  const clearHistoryMutation = useSharedClearSearchHistory({
    action: 'discover.clear_search_history',
    retryOptions: { retry: (failureCount, error) => isRetryable(error) && failureCount < 5 },
  });
  const { mutate, error } = clearHistoryMutation;
  const clear = useGatedDiscoverCall(() => mutate());
  useReportQueryFailure(error, 'clear_history');

  return { clear, error: error ?? null };
}
