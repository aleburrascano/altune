import type { UseQueryOptions } from '@tanstack/react-query';

import type { searchDiscovery } from '@shared/apiClient';
import type { DiscoveryKind, DiscoveryResult } from '@shared/catalog';
import { detailKeys } from '@shared/query';

const RESOLVE_STALE_TIME = 30 * 60 * 1000;

export interface DiscoverySearchPort {
  searchDiscovery: typeof searchDiscovery;
}

export function resolveEntityQuery(
  kind: DiscoveryKind,
  q: string,
  limit: number,
  port: DiscoverySearchPort,
): UseQueryOptions<DiscoveryResult[]> {
  return {
    queryKey: detailKeys.resolveEntity(kind, q, limit),
    queryFn: async ({ signal }) => {
      const res = await port.searchDiscovery(
        { q, kinds: [kind], limit, saveHistory: false },
        signal,
      );
      return res.results;
    },
    staleTime: RESOLVE_STALE_TIME,
  };
}
