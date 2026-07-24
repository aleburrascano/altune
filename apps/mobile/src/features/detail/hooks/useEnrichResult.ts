import { useQuery } from '@tanstack/react-query';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { resolveEntityQuery } from '../resolve-entity-query';

const norm = (s: string): string => s.toLowerCase().trim();

export function useEnrichResult(result: DiscoveryResult): {
  enriched: DiscoveryResult;
  isEnriching: boolean;
} {
  const needsEnrichment = result.sources.length === 0;
  const searchTerm = result.subtitle ? `${result.title} ${result.subtitle}` : result.title;

  const { data } = useQuery({
    ...resolveEntityQuery(result.kind, searchTerm, 5),
    enabled: needsEnrichment,
  });

  if (!needsEnrichment) {
    return { enriched: result, isEnriching: false };
  }

  if (!data?.length) {
    return { enriched: result, isEnriching: !data };
  }

  const titleNorm = norm(result.title);
  const artistNorm = result.subtitle ? norm(result.subtitle) : null;
  const match =
    data.find(
      (r) =>
        r.kind === result.kind &&
        norm(r.title) === titleNorm &&
        (artistNorm === null || (r.subtitle != null && norm(r.subtitle) === artistNorm)),
    ) ?? null;

  if (!match || match.sources.length === 0) {
    return { enriched: result, isEnriching: false };
  }

  return {
    enriched: { ...result, sources: match.sources, extras: { ...match.extras, ...result.extras } },
    isEnriching: false,
  };
}
