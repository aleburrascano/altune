import { useQuery, type QueryKey } from '@tanstack/react-query';

import type { DiscoveryKind } from '@shared/api-client/discovery';

import { isAbort } from '@shared/errors';

import { recordEnrichmentOutcome, type EnrichmentProvider } from '../detailHealth';
import { useDetailFetchEnabled } from './detailFetchGate';

const ENRICHMENT_STALE_TIME = 1000 * 60 * 60 * 24;

type EnrichmentParams = {
  kind: DiscoveryKind;
  title: string;
  subtitle?: string | null | undefined;
  mbid?: string | undefined;
  enabled?: boolean;
};

type EnrichmentReturn<T> = {
  enrichment: T | null;
  isError: boolean;
};

// A provider that fails leaves `enrichment` null — the exact shape of an entity
// that genuinely has no data there. The shared apiFetch log strips the query
// string by design, so without this line neither the entity nor the provider
// survives the failure and a retry-worthy incident is undiagnosable.
type EnrichmentFetchContext = {
  provider: EnrichmentProvider;
  kind: DiscoveryKind;
  title: string;
  subtitle: string | null;
};

// Two readings of the same outcome: the log names this entity so one incident can be
// diagnosed, the tally names only the provider so the batch's success rate can be computed.
async function fetchReportingOutcome<T>(
  fetch: () => Promise<T>,
  ctx: EnrichmentFetchContext,
): Promise<T> {
  try {
    const enrichment = await fetch();
    recordEnrichmentOutcome(ctx.provider, true);
    return enrichment;
  } catch (error) {
    if (isAbort(error)) throw error;
    recordEnrichmentOutcome(ctx.provider, false);
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
  /** Names the provider in the failure log and in the health tally. */
  provider: EnrichmentProvider;
  /** Provider fetcher; receives the resolved params and picks what it sends. */
  fetch: (params: Required<Pick<EnrichmentParams, 'kind' | 'title'>> &
    Pick<EnrichmentParams, 'subtitle' | 'mbid'> & { signal: AbortSignal }) => Promise<T>;
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
    const isFetchEnabled = useDetailFetchEnabled();
    const hasLookupKey = title.trim() !== '' || hasMbid;
    const canFetch = enabled && isFetchEnabled && hasLookupKey;
    const { data, isError } = useQuery<T>({
      queryKey: [config.keyPrefix, kind, cacheKey] as QueryKey,
      queryFn: ({ signal }) =>
        fetchReportingOutcome(() => config.fetch({ kind, title, subtitle, mbid, signal }), {
          provider: config.provider,
          kind,
          title,
          subtitle: subtitle ?? null,
        }),
      enabled: canFetch,
      staleTime: ENRICHMENT_STALE_TIME,
    });

    return { enrichment: data && data.has_content ? data : null, isError };
  };
}
