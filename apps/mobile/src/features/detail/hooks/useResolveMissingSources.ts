import { useQuery } from '@tanstack/react-query';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { resolveEntityQuery } from '../resolve-entity-query';
import { normalizeForCompare } from '../text-compare';

export function useResolveMissingSources(result: DiscoveryResult): {
  resolved: DiscoveryResult;
  isResolving: boolean;
} {
  const needsSources = result.sources.length === 0;
  const searchTerm = result.subtitle ? `${result.title} ${result.subtitle}` : result.title;

  const { data } = useQuery({
    ...resolveEntityQuery(result.kind, searchTerm, 5),
    enabled: needsSources,
  });

  if (!needsSources) {
    return { resolved: result, isResolving: false };
  }

  if (!data?.length) {
    return { resolved: result, isResolving: !data };
  }

  const titleNorm = normalizeForCompare(result.title);
  const artistNorm = result.subtitle ? normalizeForCompare(result.subtitle) : null;
  const match =
    data.find(
      (r) =>
        r.kind === result.kind &&
        normalizeForCompare(r.title) === titleNorm &&
        (artistNorm === null ||
          (r.subtitle != null && normalizeForCompare(r.subtitle) === artistNorm)),
    ) ?? null;

  if (!match || match.sources.length === 0) {
    return { resolved: result, isResolving: false };
  }

  return {
    resolved: { ...result, sources: match.sources, extras: { ...match.extras, ...result.extras } },
    isResolving: false,
  };
}
