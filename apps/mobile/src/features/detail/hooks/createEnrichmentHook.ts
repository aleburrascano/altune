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

type EnrichmentFetchContext = {
  provider: EnrichmentProvider;
  kind: DiscoveryKind;
  title: string;
  subtitle: string | null;
};

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
  keyPrefix: string;
  provider: EnrichmentProvider;
  fetch: (
    params: Required<Pick<EnrichmentParams, 'kind' | 'title'>> &
      Pick<EnrichmentParams, 'subtitle' | 'mbid'> & { signal: AbortSignal },
  ) => Promise<T>;
  mbidAware: boolean;
};

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
    const { data: enrichment, isError } = useQuery<T>({
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

    return { enrichment: enrichment && enrichment.has_content ? enrichment : null, isError };
  };
}
