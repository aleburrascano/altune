import { useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { resolveEntityQuery } from '../resolve-entity-query';
import { normalizeForCompare } from '../text-compare';
import { useDetailFetchEnabled } from './detailFetchGate';

// A failed resolve leaves no candidates — the exact shape of an entity that
// genuinely has no external sources — so without this line the two are
// indistinguishable on the client and a broken search needs a live repro to
// find, for an entity nobody can name afterwards.
function useLoggedResolveFailure(result: DiscoveryResult, error: Error | null): void {
  useEffect(() => {
    if (error === null) {
      return;
    }
    console.warn('[detail] source resolution fetch failed', {
      kind: result.kind,
      title: result.title,
      subtitle: result.subtitle ?? null,
      error: error.message,
    });
  }, [error, result.kind, result.title, result.subtitle]);
}

export function useResolveMissingSources(result: DiscoveryResult): {
  resolved: DiscoveryResult;
  isResolving: boolean;
} {
  const needsSources = result.sources.length === 0;
  const fetchEnabled = useDetailFetchEnabled();
  const searchTerm = result.subtitle ? `${result.title} ${result.subtitle}` : result.title;

  const { data, isLoading, error } = useQuery({
    ...resolveEntityQuery(result.kind, searchTerm, 5),
    enabled: needsSources && fetchEnabled,
  });

  useLoggedResolveFailure(result, error);

  if (!needsSources) {
    return { resolved: result, isResolving: false };
  }

  if (!data?.length) {
    return { resolved: result, isResolving: isLoading };
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
