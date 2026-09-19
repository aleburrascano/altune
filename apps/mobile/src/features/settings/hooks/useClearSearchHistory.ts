import { useMutation, useQueryClient } from '@tanstack/react-query';

import { isRetryable } from '@shared/api-client';
import { clearSearchHistory } from '@shared/api-client/discovery';
import { guardedMutationOptions } from '@shared/session/signOutCleanup';
import { discoveryKeys } from '@shared/lib/query-keys';
import { retryDelayMs } from '@shared/query/retryDelay';

export function useClearSearchHistory() {
  const queryClient = useQueryClient();
  return useMutation({
    ...guardedMutationOptions({
      mutationFn: clearSearchHistory,
      onMutate: () => {
        // An in-flight history refetch must not land on top of the optimistic clear.
        void queryClient.cancelQueries({ queryKey: discoveryKeys.history });
        queryClient.setQueryData(discoveryKeys.history, { items: [] });
        return {};
      },
      onError: () => {
        void queryClient.invalidateQueries({ queryKey: discoveryKeys.history });
      },
    }),
    // Mutations get no retry by default; the DELETE is idempotent, so retry
    // transient failures on the same policy and jittered backoff queries use
    // (_layout.tsx). Un-jittered, every client failing on one outage retries together.
    retry: (failureCount, error) => isRetryable(error) && failureCount < 5,
    retryDelay: (failureCount) => retryDelayMs(failureCount, Math.random()),
  });
}
