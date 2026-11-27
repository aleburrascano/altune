import { useQuery, useQueryClient, type UseMutationResult } from '@tanstack/react-query';
import type { DiscoverySearchHistoryResponse } from '@shared/catalog';
import { isRetryable } from '@shared/failure';
import { discoveryKeys, refetchWithoutEvent, useAppMutation } from '@shared/query';
import { useClearSearchHistory as useSharedClearSearchHistory } from '@shared/searchHistory';
import { useReportQueryFailure } from '@features/discovery/view/search/useReportQueryFailure';
import { useDiscoverPort } from '@features/discovery/view/DiscoverPortProvider';

export function useSearchHistory() {
  const port = useDiscoverPort();

  const {
    data: response,
    error,
    refetch: refetchHistory,
  } = useQuery<DiscoverySearchHistoryResponse>({
    queryKey: discoveryKeys.history,
    queryFn: ({ signal }) => port.listSearchHistory({ limit: 10 }, signal),
  });

  useReportQueryFailure(error, 'history');

  return { data: response, error, refetch: () => void refetchHistory() };
}

export function useRecordSearchHistory(): (query: string) => void {
  const { mutate, error } = useRecordHistoryMutation();
  useReportQueryFailure(error, 'record_history');
  return (query) => mutate(query);
}

function useRecordHistoryMutation(): UseMutationResult<void, Error, string, undefined> {
  const port = useDiscoverPort();
  const queryClient = useQueryClient();
  return useAppMutation({
    action: 'discover.record_search_history',
    mutationFn: (query: string) => port.recordSearchHistory(query),
    onSuccess: () => refetchWithoutEvent(queryClient, [discoveryKeys.history]),
  });
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
  useReportQueryFailure(error, 'clear_history');

  return { clear: () => mutate(), error: error ?? null };
}
