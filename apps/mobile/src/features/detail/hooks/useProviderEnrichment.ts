import { useQuery } from '@tanstack/react-query';

import type { DiscoveryKind } from '@shared/api-client/discovery';

import { isAbort } from '@shared/errors';

import {
  recordEnrichmentOutcome,
  type EnrichmentProvider,
} from '@/features/detail/domain/detailHealth';
import type { DetailPort } from '@/features/detail/ports/detailPort';
import { useDetailPort } from '@/features/detail/ports/DetailPortProvider';
import { useDetailFetchEnabled } from '@/features/detail/hooks/useDetailFetchEnabled';

const ONE_DAY_MS = 86_400_000;
const ENRICHMENT_STALE_TIME = ONE_DAY_MS;

export type EnrichmentParams = {
  kind: DiscoveryKind;
  title: string;
  subtitle?: string | null | undefined;
  mbid?: string | undefined;
  enabled?: boolean;
};

export type EnrichmentReturn<T> = {
  enrichment: T | null;
  isError: boolean;
};

type HasContent = { has_content: boolean };

type EnrichmentSignal = { signal: AbortSignal };

type EnrichmentFetchContext = {
  provider: EnrichmentProvider;
  kind: DiscoveryKind;
  title: string;
  subtitle: string | null;
};

type EnrichmentFetchParams = Required<Pick<EnrichmentParams, 'kind' | 'title'>> &
  Pick<EnrichmentParams, 'subtitle' | 'mbid'> &
  EnrichmentSignal;

export type EnrichmentHookConfig<T> = {
  keyPrefix: string;
  provider: EnrichmentProvider;
  fetch: (port: DetailPort, params: EnrichmentFetchParams) => Promise<T>;
  mbidAware: boolean;
};

type EnrichmentRequest<T> = {
  config: EnrichmentHookConfig<T>;
  port: DetailPort;
  params: EnrichmentParams;
};

function reportEnrichmentFailure(context: EnrichmentFetchContext, error: unknown): void {
  if (isAbort(error)) return;
  recordEnrichmentOutcome(context.provider, false);
  console.warn('[detail] enrichment fetch failed', {
    ...context,
    error: error instanceof Error ? error.message : String(error),
  });
}

async function fetchReportingOutcome<T>(fetch: () => Promise<T>, ctx: EnrichmentFetchContext) {
  try {
    const enrichment = await fetch();
    recordEnrichmentOutcome(ctx.provider, true);
    return enrichment;
  } catch (error) {
    reportEnrichmentFailure(ctx, error);
    throw error;
  }
}

function enrichmentMbid(config: EnrichmentHookConfig<unknown>, params: EnrichmentParams) {
  return config.mbidAware && params.mbid ? params.mbid : null;
}

function enrichmentCacheKey(config: EnrichmentHookConfig<unknown>, params: EnrichmentParams) {
  return enrichmentMbid(config, params) ?? `${params.title}|${params.subtitle ?? ''}`;
}

function hasLookupKey(config: EnrichmentHookConfig<unknown>, params: EnrichmentParams) {
  return params.title.trim() !== '' || enrichmentMbid(config, params) !== null;
}

function visibleEnrichment<T extends HasContent>(enrichment: T | undefined): T | null {
  return enrichment && enrichment.has_content ? enrichment : null;
}

function fetchContext(provider: EnrichmentProvider, params: EnrichmentParams) {
  const { kind, title, subtitle } = params;
  return { provider, kind, title, subtitle: subtitle ?? null };
}

function enrichmentQueryFn<T>({ config, port, params }: EnrichmentRequest<T>) {
  return ({ signal }: EnrichmentSignal) =>
    fetchReportingOutcome(
      () => config.fetch(port, { ...params, signal }),
      fetchContext(config.provider, params),
    );
}

function enrichmentQueryOptions<T>(request: EnrichmentRequest<T>, isFetchEnabled: boolean) {
  const { config, params } = request;
  return {
    queryKey: [config.keyPrefix, params.kind, enrichmentCacheKey(config, params)],
    queryFn: enrichmentQueryFn(request),
    enabled: (params.enabled ?? true) && isFetchEnabled && hasLookupKey(config, params),
    staleTime: ENRICHMENT_STALE_TIME,
  };
}

export function useProviderEnrichment<T extends HasContent>(
  config: EnrichmentHookConfig<T>,
  params: EnrichmentParams,
): EnrichmentReturn<T> {
  const port = useDetailPort();
  const isFetchEnabled = useDetailFetchEnabled();
  const query = useQuery<T>(enrichmentQueryOptions({ config, port, params }, isFetchEnabled));
  return { enrichment: visibleEnrichment(query.data), isError: query.isError };
}
