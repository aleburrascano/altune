import { QueryClient } from '@tanstack/react-query';

import { transientRetryOptions } from '@shared/query/retryDelay';
import { appMutationCache, appQueryCache } from '@shared/query/useAppMutation';

const QUERY_STALE_TIME_MS = 30_000;

const appQueryDefaults = { staleTime: QUERY_STALE_TIME_MS, ...transientRetryOptions };

export function createQueryClient(): QueryClient {
  return new QueryClient({
    queryCache: appQueryCache(),
    mutationCache: appMutationCache(),
    defaultOptions: { queries: appQueryDefaults },
  });
}
