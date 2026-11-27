import { QueryClient } from '@tanstack/react-query';

import { STALE } from '@shared/query/core/stalePolicy';
import { transientRetryOptions } from '@shared/query/core/retryDelay';
import { appMutationCache, appQueryCache } from '@shared/query/shell/useAppMutation';

const appQueryDefaults = { staleTime: STALE.default, ...transientRetryOptions };

export function createQueryClient(): QueryClient {
  return new QueryClient({
    queryCache: appQueryCache(),
    mutationCache: appMutationCache(),
    defaultOptions: { queries: appQueryDefaults },
  });
}
