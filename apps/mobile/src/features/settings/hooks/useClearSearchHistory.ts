/**
 * useClearSearchHistory — the Settings-owned "clear recent searches" action.
 *
 * Same server delete Discover's inline Clear performs, reached from where users
 * actually look for it. Optimistically empties the cache so the Discover empty
 * state is already clean when they navigate back; on failure, invalidates so the
 * still-populated history reappears rather than lying about being cleared.
 */
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { clearSearchHistory } from '@shared/api-client/discovery';
import { discoveryKeys } from '@shared/lib/query-keys';

export function useClearSearchHistory() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: clearSearchHistory,
    onMutate: () => {
      queryClient.setQueryData(discoveryKeys.history, { items: [] });
    },
    onError: () => {
      void queryClient.invalidateQueries({ queryKey: discoveryKeys.history });
    },
  });
}
