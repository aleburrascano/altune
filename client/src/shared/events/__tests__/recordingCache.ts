import type { QueryClient } from '@tanstack/react-query';

import type { EventCache } from '@shared/events';

export interface RecordingCache extends EventCache {
  readonly invalidated: unknown[];
  readonly reads: unknown[];
  readonly writes: unknown[];
}

export function recordingCache(queryClient: QueryClient): RecordingCache {
  const invalidated: unknown[] = [];
  const reads: unknown[] = [];
  const writes: unknown[] = [];
  return {
    invalidated,
    reads,
    writes,
    invalidateQueries(filters) {
      invalidated.push(filters.queryKey);
      return queryClient.invalidateQueries(filters);
    },
    getQueryData(queryKey) {
      reads.push(queryKey);
      return queryClient.getQueryData(queryKey);
    },
    setQueryData(queryKey, updater) {
      writes.push(queryKey);
      return queryClient.setQueryData(queryKey, updater);
    },
    getQueriesData: (filters) => queryClient.getQueriesData(filters),
    setQueriesData: (filters, updater) => queryClient.setQueriesData(filters, updater),
    getQueryCache: () => queryClient.getQueryCache(),
  };
}
