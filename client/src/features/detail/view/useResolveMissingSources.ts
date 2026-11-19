import { useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { DiscoveryResult } from '@shared/catalog';
import { resolveEntityQuery } from '@features/detail/core/resolveEntityQuery';
import { normalizeForCompare } from '@features/detail/core/normalizeForCompare';
import { realPhotoUrl } from '@features/detail/core/artistPhoto';
import type { DetailPort } from '@features/detail/ports/detailPort';
import { useDetailPort } from '@features/detail/view/DetailPortProvider';

type ResolveTarget = Pick<DiscoveryResult, 'kind' | 'title' | 'subtitle'>;

function warnResolveFailure(port: DetailPort, target: ResolveTarget, error: Error): void {
  port.warn('[detail] source resolution fetch failed', {
    kind: target.kind,
    title: target.title,
    subtitle: target.subtitle ?? null,
    error: error.message,
  });
}

function useLoggedResolveFailure(entity: DiscoveryResult, error: Error | null): void {
  const port = useDetailPort();
  const { kind, title, subtitle } = entity;
  useEffect(() => {
    if (error === null) {
      return;
    }
    warnResolveFailure(port, { kind, title, subtitle }, error);
  }, [port, error, kind, title, subtitle]);
}

function useResolveSearch(entity: DiscoveryResult, enabled: boolean) {
  const port = useDetailPort();
  const searchTerm = entity.subtitle ? `${entity.title} ${entity.subtitle}` : entity.title;
  return useQuery({
    ...resolveEntityQuery(entity.kind, searchTerm, 5, port),
    enabled,
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
  return {
    ...entity,
    image_url: realPhotoUrl(entity.image_url) ?? realPhotoUrl(match.image_url),
    sources: match.sources,
    extras: { ...match.extras, ...entity.extras },
  };
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
