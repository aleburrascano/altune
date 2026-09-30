import { useQueryClient } from '@tanstack/react-query';

import { useAppMutation } from '@shared/query/useAppMutation';

import {
  clearSearchHistory,
  type DiscoverySearchHistoryResponse,
} from '@shared/api-client/discovery';
import { guardedMutationOptions } from '@shared/session/signOutCleanup';
import { discoveryKeys } from '@shared/lib/query-keys';
import { transientRetryOptions } from '@shared/query/retryDelay';

export function useClearSearchHistory() {
  const queryClient = useQueryClient();
  return useAppMutation({
    action: 'settings.clear_search_history',
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
    ...transientRetryOptions,
  });
}
