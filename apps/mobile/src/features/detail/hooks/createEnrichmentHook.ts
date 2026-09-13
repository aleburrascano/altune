import type { DiscoveryKind } from '@shared/api-client/discovery';

import { useEnrichmentQuery } from './useEnrichmentQuery';

type EnrichmentParams = {
  kind: DiscoveryKind;
  title: string;
  subtitle?: string | null | undefined;
  mbid?: string | undefined;
  enabled?: boolean;
};

type EnrichmentReturn<T> = {
  enrichment: T | null;
  isLoading: boolean;
  isError: boolean;
};

type EnrichmentHookConfig<T> = {
  /** react-query cache-key namespace for this provider. */
  keyPrefix: string;
  /** Provider fetcher; receives the resolved params and picks what it sends. */
  fetch: (params: Required<Pick<EnrichmentParams, 'kind' | 'title'>> &
    Pick<EnrichmentParams, 'subtitle' | 'mbid'>) => Promise<T>;
  /**
   * When true, a present mbid drives the cache key and can enable the query on
   * its own (MusicBrainz). When false, the query keys on `title|subtitle` and
   * enables on title alone (Deezer, Last.fm). This single flag is what used to
   * silently drift between the three hand-copied hook bodies.
   */
  mbidAware: boolean;
};

/**
 * Builds a single-provider enrichment hook. Collapses the previously
 * hand-copied `useEnrichment` / `useDeezerEnrichment` / `useLastFmEnrichment`
 * bodies into one place so their cache-key and enable logic cannot diverge.
 */
export function createEnrichmentHook<T extends { has_content: boolean }>(
  config: EnrichmentHookConfig<T>,
) {
  return function useProviderEnrichment({
    kind,
    title,
    subtitle,
    mbid,
    enabled = true,
  }: EnrichmentParams): EnrichmentReturn<T> {
    const hasMbid = config.mbidAware && mbid !== undefined && mbid !== '';
    const cacheKey = hasMbid ? mbid : `${title}|${subtitle ?? ''}`;
    const { value, isLoading, isError } = useEnrichmentQuery({
      queryKey: [config.keyPrefix, kind, cacheKey],
      queryFn: () => config.fetch({ kind, title, subtitle, mbid }),
      hasContent: (e) => e.has_content,
      enabled: enabled && (title.trim() !== '' || hasMbid),
    });

    return { enrichment: value, isLoading, isError };
  };
}
