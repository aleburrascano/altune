import { useQueryClient, type QueryClient, type UseMutationOptions } from '@tanstack/react-query';

import { useAppMutation } from '@shared/query/useAppMutation';

import {
  clearSearchHistory,
  type DiscoverySearchHistoryResponse,
} from '@shared/api-client/discovery';
import { guardedMutationOptions } from '@shared/session/signOutCleanup';
import { discoveryKeys } from '@shared/lib/query-keys';

type ClearSearchHistoryOptions = {
  action: string;
  retryOptions: Pick<UseMutationOptions<void, Error, void>, 'retry' | 'retryDelay'>;
};

function emptyHistory(queryClient: QueryClient) {
  queryClient.setQueryData(discoveryKeys.history, { items: [] });
}

function optimisticClear(queryClient: QueryClient) {
  void queryClient.cancelQueries({ queryKey: discoveryKeys.history });
  const previous = queryClient.getQueryData<DiscoverySearchHistoryResponse>(discoveryKeys.history);
  emptyHistory(queryClient);
  return { previous };
}

async function confirmClear(queryClient: QueryClient) {
  await queryClient.cancelQueries({ queryKey: discoveryKeys.history });
  emptyHistory(queryClient);
}

function rollbackClear(
  queryClient: QueryClient,
  previous: DiscoverySearchHistoryResponse | undefined,
) {
  if (previous !== undefined) {
    queryClient.setQueryData(discoveryKeys.history, previous);
  }
  void queryClient.invalidateQueries({ queryKey: discoveryKeys.history });
}

function clearMutationOptions(queryClient: QueryClient) {
  return guardedMutationOptions({
    mutationFn: clearSearchHistory,
    onMutate: () => optimisticClear(queryClient),
    onSuccess: () => confirmClear(queryClient),
    onError: (_error, _vars, context) => rollbackClear(queryClient, context.previous),
  });
}

export function useClearSearchHistory({ action, retryOptions }: ClearSearchHistoryOptions) {
  const queryClient = useQueryClient();
  return useAppMutation({ action, ...clearMutationOptions(queryClient), ...retryOptions });
}
