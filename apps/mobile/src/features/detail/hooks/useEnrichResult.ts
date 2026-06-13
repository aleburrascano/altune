import { useQuery } from '@tanstack/react-query';

import { searchDiscovery, type DiscoveryResult } from '@shared/api-client/discovery';

export function useEnrichResult(result: DiscoveryResult): {
  enriched: DiscoveryResult;
  isEnriching: boolean;
} {
  const needsEnrichment = result.sources.length === 0;
  const searchTerm = result.kind === 'album' && result.subtitle
    ? `${result.title} ${result.subtitle}`
    : result.title;

  const { data } = useQuery({
    queryKey: ['enrich', result.kind, searchTerm],
    queryFn: () => searchDiscovery({
      q: searchTerm,
      kinds: [result.kind],
      limit: 5,
      saveHistory: false,
    }),
    enabled: needsEnrichment,
    staleTime: 1000 * 60 * 30,
  });

  if (!needsEnrichment) {
    return { enriched: result, isEnriching: false };
  }

  if (!data?.results?.length) {
    return { enriched: result, isEnriching: !data };
  }

  const titleNorm = result.title.toLowerCase().trim();
  const match = data.results.find(
    (r) => r.kind === result.kind && r.title.toLowerCase().trim() === titleNorm,
  ) ?? data.results.find((r) => r.kind === result.kind) ?? null;

  if (!match || match.sources.length === 0) {
    return { enriched: result, isEnriching: false };
  }

  return {
    enriched: { ...result, sources: match.sources, extras: { ...result.extras, ...match.extras } },
    isEnriching: false,
  };
}
