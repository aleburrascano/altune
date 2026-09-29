import { useQueryClient } from '@tanstack/react-query';

import { useAppMutation } from '@shared/query/useAppMutation';

import { isRetryable } from '@shared/api-client';
import {
  clearSearchHistory,
  type DiscoverySearchHistoryResponse,
} from '@shared/api-client/discovery';
import { guardedMutationOptions } from '@shared/session/signOutCleanup';
import { discoveryKeys } from '@shared/lib/query-keys';
import { useReportQueryFailure } from '@shared/telemetry/useReportQueryFailure';
import { useGatedDiscoverCall } from './discoverFetchGate';

export type ClearSearchHistory = {
  clear: () => void;
  error: Error | null;
};

export function useClearSearchHistory(): ClearSearchHistory {
  const queryClient = useQueryClient();
  const clearHistoryMutation = useAppMutation({
    action: 'discover.clear_search_history',
    ...guardedMutationOptions({
      mutationFn: clearSearchHistory,
      onMutate: () => {
        void queryClient.cancelQueries({ queryKey: discoveryKeys.history });
        const previous = queryClient.getQueryData<DiscoverySearchHistoryResponse>(
          discoveryKeys.history,
        );
        queryClient.setQueryData(discoveryKeys.history, { items: [] });
        return { previous };
      },
      onSuccess: async () => {
        await queryClient.cancelQueries({ queryKey: discoveryKeys.history });
        queryClient.setQueryData(discoveryKeys.history, { items: [] });
      },
      onError: (_error, _vars, context) => {
        if (context.previous !== undefined) {
          queryClient.setQueryData(discoveryKeys.history, context.previous);
        }
        void queryClient.invalidateQueries({ queryKey: discoveryKeys.history });
      },
    }),
    retry: (failureCount, error) => isRetryable(error) && failureCount < 5,
  });
  const { mutate, error } = clearHistoryMutation;
  const clear = useGatedDiscoverCall(() => mutate());
  useReportQueryFailure(error, 'clear_history');

  return { clear, error: error ?? null };
}
