import { queryOptions } from '@tanstack/react-query';

import type { DiscoveryKind, DiscoveryResult, searchDiscovery } from '@shared/apiClient/discovery';

const RESOLVE_STALE_TIME = 30 * 60 * 1000;

export interface DiscoverySearchPort {
  searchDiscovery: typeof searchDiscovery;
}

export function resolveEntityQuery(
  kind: DiscoveryKind,
  q: string,
  limit: number,
  port: DiscoverySearchPort,
): ReturnType<typeof queryOptions<DiscoveryResult[]>> {
  return queryOptions<DiscoveryResult[]>({
    queryKey: ['resolve-entity', kind, q, limit],
    queryFn: async ({ signal }) => {
      const res = await port.searchDiscovery(
        { q, kinds: [kind], limit, saveHistory: false },
        signal,
      );
      return res.results;
    },
    staleTime: RESOLVE_STALE_TIME,
  });
}
