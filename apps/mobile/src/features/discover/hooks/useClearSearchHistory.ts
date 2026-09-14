import { useMutation, useQueryClient } from '@tanstack/react-query';

import {
  clearSearchHistory,
  type DiscoverySearchHistoryResponse,
} from '@shared/api-client/discovery';
import { currentSessionEpoch, isSameSession } from '@shared/auth/signOutCleanup';
import { discoveryKeys } from '@shared/lib/query-keys';

export type ClearSearchHistory = {
  clear: () => void;
  /** Set when the last clear failed and the history was rolled back; null otherwise. */
  error: Error | null;
};

/**
 * Clears search history optimistically: the cached list empties at once, and a
 * failed server call rolls it back to the snapshot, surfaces the error, and
 * re-fetches to reconcile with the server.
 */
export function useClearSearchHistory(): ClearSearchHistory {
  const queryClient = useQueryClient();
  const clearHistoryMutation = useMutation({
    mutationFn: clearSearchHistory,
    onMutate: () => {
      // An in-flight history refetch must not land on top of the optimistic clear.
      void queryClient.cancelQueries({ queryKey: discoveryKeys.history });
      const previous = queryClient.getQueryData<DiscoverySearchHistoryResponse>(
        discoveryKeys.history,
      );
      queryClient.setQueryData(discoveryKeys.history, { items: [] });
      return { previous, epoch: currentSessionEpoch() };
    },
    onError: (_error, _vars, context) => {
      // A failure that settles after sign-out must not restore or refetch into the next user's cache.
      if (!isSameSession(context?.epoch)) return;
      if (context?.previous !== undefined) {
        queryClient.setQueryData(discoveryKeys.history, context.previous);
      }
      void queryClient.invalidateQueries({ queryKey: discoveryKeys.history });
    },
  });
  const { mutate, error } = clearHistoryMutation;
  return {
    clear: (): void => {
      mutate();
    },
    error: error ?? null,
  };
}
