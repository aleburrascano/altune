import { useMutation, useQueryClient } from '@tanstack/react-query';

import { isRetryable } from '@shared/api-client';
import { clearSearchHistory } from '@shared/api-client/discovery';
import { currentSessionEpoch, isSameSession } from '@shared/session/signOutCleanup';
import { discoveryKeys } from '@shared/lib/query-keys';
import { retryDelayMs } from '@shared/query/retryDelay';

export function useClearSearchHistory() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: clearSearchHistory,
    // Mutations get no retry by default; the DELETE is idempotent, so retry
    // transient failures on the same policy and jittered backoff queries use
    // (_layout.tsx). Un-jittered, every client failing on one outage retries together.
    retry: (failureCount, error) => isRetryable(error) && failureCount < 5,
    retryDelay: (failureCount) => retryDelayMs(failureCount, Math.random()),
    onMutate: () => {
      // An in-flight history refetch must not land on top of the optimistic clear.
      void queryClient.cancelQueries({ queryKey: discoveryKeys.history });
      queryClient.setQueryData(discoveryKeys.history, { items: [] });
      return { epoch: currentSessionEpoch() };
    },
    onError: (_error, _vars, context) => {
      // A failure that settles after sign-out must not refetch the next user's history.
      if (!isSameSession(context?.epoch)) return;
      void queryClient.invalidateQueries({ queryKey: discoveryKeys.history });
    },
  });
}
