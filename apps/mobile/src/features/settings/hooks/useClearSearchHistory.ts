import { useMutation, useQueryClient } from '@tanstack/react-query';

import { clearSearchHistory } from '@shared/api-client/discovery';
import { currentSessionEpoch, isSameSession } from '@shared/auth/signOutCleanup';
import { discoveryKeys } from '@shared/lib/query-keys';

export function useClearSearchHistory() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: clearSearchHistory,
    onMutate: () => {
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
