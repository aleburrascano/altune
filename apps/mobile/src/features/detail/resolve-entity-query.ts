/**
 * resolveEntityQuery — the one cache identity for "resolve an entity by name
 * via discovery search".
 *
 * Lateral nav, artist/album discovery, and library enrichment all issue the
 * same `searchDiscovery({q, kinds, limit, saveHistory: false})` call; before
 * this factory each cached it under its own key (or, for lateral nav, not at
 * all), so one lateral hop hit the backend twice for one answer. One key shape
 * means one fetch per (kind, query, limit) per 30 minutes, however the lookup
 * is reached.
 */

import { queryOptions } from '@tanstack/react-query';

import { searchDiscovery, type DiscoveryKind, type DiscoveryResult } from '@shared/api-client/discovery';

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
