import { useQueryClient } from '@tanstack/react-query';

import { useAppMutation } from '@shared/query/useAppMutation';

import { clearSearchHistory } from '@shared/api-client/discovery';
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
        queryClient.setQueryData(discoveryKeys.history, { items: [] });
        return {};
      },
      onError: () => {
        void queryClient.invalidateQueries({ queryKey: discoveryKeys.history });
      },
    }),
    ...transientRetryOptions,
  });
}
