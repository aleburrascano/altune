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
