import { queryOptions } from '@tanstack/react-query';

import type { DiscoveryKind, DiscoveryResult } from '@shared/api-client/discovery';

import type { DetailPort } from './ports/detailPort';

const RESOLVE_STALE_TIME = 30 * 60 * 1000;

export function resolveEntityQuery(
  kind: DiscoveryKind,
  q: string,
  limit: number,
  port: DetailPort,
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
