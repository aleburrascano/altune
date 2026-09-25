import { useMutation, useQueryClient } from '@tanstack/react-query';

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
  /** Set when the last clear failed and the history was rolled back; null otherwise. */
  error: Error | null;
};

/**
 * Clears search history optimistically: the cached list empties at once, a
 * successful call re-asserts that over any refetch that raced it, and a failed
 * one rolls back to the snapshot, surfaces the error, and re-fetches to
 * reconcile with the server.
 */
export function useClearSearchHistory(): ClearSearchHistory {
  const queryClient = useQueryClient();
  const clearHistoryMutation = useMutation({
    ...guardedMutationOptions({
      mutationFn: clearSearchHistory,
      onMutate: () => {
        // An in-flight history refetch must not land on top of the optimistic clear.
        void queryClient.cancelQueries({ queryKey: discoveryKeys.history });
        const previous = queryClient.getQueryData<DiscoverySearchHistoryResponse>(
          discoveryKeys.history,
        );
        queryClient.setQueryData(discoveryKeys.history, { items: [] });
        return { previous };
      },
      onSuccess: async () => {
        // Discover invalidates this key on every settled search, so a refetch started
        // after onMutate can still carry the pre-clear list: the clear writes last.
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
    // Mutations get no retry by default; the DELETE is idempotent, so retry
    // transient failures with the same policy settings' copy of this uses.
    retry: (failureCount, error) => isRetryable(error) && failureCount < 5,
  });
  const { mutate, error } = clearHistoryMutation;
  // A mutation has no `enabled` to switch off, so the gate sits on the affordance: while discovery
  // is off the optimistic clear never runs either, and the cached history is left as it stands.
  const clear = useGatedDiscoverCall(() => mutate());
  useReportQueryFailure(error, 'clear_history');

  return { clear, error: error ?? null };
}
