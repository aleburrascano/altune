import { queryOptions } from '@tanstack/react-query';

import {
  searchDiscovery,
  type DiscoveryKind,
  type DiscoveryResult,
} from '@shared/api-client/discovery';

const RESOLVE_STALE_TIME = 30 * 60 * 1000;

export function resolveEntityQuery(
  kind: DiscoveryKind,
  q: string,
  limit: number,
): ReturnType<typeof queryOptions<DiscoveryResult[]>> {
  return queryOptions<DiscoveryResult[]>({
    queryKey: ['resolve-entity', kind, q, limit],
    queryFn: async () => {
      const res = await searchDiscovery({ q, kinds: [kind], limit, saveHistory: false });
      return res.results;
    },
    staleTime: RESOLVE_STALE_TIME,
  });
}
