import { useMutation, useQueryClient } from '@tanstack/react-query';

import { clearSearchHistory } from '@shared/api-client/discovery';
import { discoveryKeys } from '@shared/lib/query-keys';

/**
 * Clears search history optimistically: the cached list empties at once, and a
 * failed server call re-fetches the real history.
 */
export function useClearSearchHistory(): () => void {
  const queryClient = useQueryClient();
  const clearHistoryMutation = useMutation({
    mutationFn: clearSearchHistory,
    onError: () => {
      void queryClient.invalidateQueries({ queryKey: discoveryKeys.history });
    },
  });
  return (): void => {
    queryClient.setQueryData(discoveryKeys.history, { items: [] });
    clearHistoryMutation.mutate();
  };
}
