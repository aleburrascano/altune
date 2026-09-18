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

// A provider that fails leaves `enrichment` null — the exact shape of an entity
// that genuinely has no data there. The shared apiFetch log strips the query
// string by design, so without this line neither the entity nor the provider
// survives the failure and a retry-worthy incident is undiagnosable.
type EnrichmentFetchContext = {
  provider: string;
  kind: DiscoveryKind;
  title: string;
  subtitle: string | null;
};

async function fetchLoggingFailure<T>(
  fetch: () => Promise<T>,
  ctx: EnrichmentFetchContext,
): Promise<T> {
  try {
    return await fetch();
  } catch (error) {
    console.warn('[detail] enrichment fetch failed', {
      ...ctx,
      error: error instanceof Error ? error.message : String(error),
    });
    throw error;
  }
}

type EnrichmentHookConfig<T> = {
  /** react-query cache-key namespace for this provider. */
  keyPrefix: string;
  /** Names the provider in the failure log; matches the `EnrichmentErrors` keys. */
  provider: string;
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
      queryFn: () =>
        fetchLoggingFailure(() => config.fetch({ kind, title, subtitle, mbid }), {
          provider: config.provider,
          kind,
          title,
          subtitle: subtitle ?? null,
        }),
      hasContent: (e) => e.has_content,
      enabled: enabled && (title.trim() !== '' || hasMbid),
    });

    return { enrichment: value, isLoading, isError };
  };
}
