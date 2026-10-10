import { useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';

import type { DiscoveryResult } from '@shared/apiClient/discovery';

import { resolveEntityQuery } from '@/features/detail/domain/resolveEntityQuery';
import { normalizeForCompare } from '@/features/detail/domain/normalizeForCompare';
import { useDetailPort } from '@/features/detail/ports/DetailPortProvider';
import { useDetailFetchEnabled } from '@/features/detail/hooks/useDetailFetchGate';

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

function useResolveSearch(entity: DiscoveryResult, enabled: boolean) {
  const port = useDetailPort();
  const fetchEnabled = useDetailFetchEnabled();
  const searchTerm = entity.subtitle ? `${entity.title} ${entity.subtitle}` : entity.title;
  return useQuery({
    ...resolveEntityQuery(entity.kind, searchTerm, 5, port),
    enabled: enabled && fetchEnabled,
  });
}

function sameArtist(entity: DiscoveryResult, other: DiscoveryResult): boolean {
  if (!entity.subtitle) return true;
  return (
    other.subtitle != null &&
    normalizeForCompare(other.subtitle) === normalizeForCompare(entity.subtitle)
  );
}

function findMatch(entity: DiscoveryResult, candidates: DiscoveryResult[]): DiscoveryResult | null {
  const titleNorm = normalizeForCompare(entity.title);
  const matches = (r: DiscoveryResult) =>
    r.kind === entity.kind && normalizeForCompare(r.title) === titleNorm && sameArtist(entity, r);
  return candidates.find(matches) ?? null;
}

function withSourcesFrom(entity: DiscoveryResult, match: DiscoveryResult | null): DiscoveryResult {
  if (!match || match.sources.length === 0) return entity;
  return { ...entity, sources: match.sources, extras: { ...match.extras, ...entity.extras } };
}

type Resolution = { resolved: DiscoveryResult; isResolving: boolean };

export function useResolveMissingSources(entity: DiscoveryResult): Resolution {
  const needsSources = entity.sources.length === 0;
  const { data: found, isLoading, error } = useResolveSearch(entity, needsSources);
  useLoggedResolveFailure(entity, error);
  if (!needsSources) return { resolved: entity, isResolving: false };
  if (!found?.length) return { resolved: entity, isResolving: isLoading };
  return { resolved: withSourcesFrom(entity, findMatch(entity, found)), isResolving: false };
}
