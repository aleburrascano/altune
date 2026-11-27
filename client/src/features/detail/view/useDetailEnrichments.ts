import { useQuery } from '@tanstack/react-query';

import type { DeezerEnrichmentResponse, EnrichmentResponse } from '@shared/apiClient';
import type { DiscoveryKind, DiscoveryResult } from '@shared/catalog';
import { detailKeys, STALE } from '@shared/query';
import { isAbort } from '@shared/failure';
import {
  recordEnrichmentOutcome,
  type EnrichmentProvider,
} from '@features/detail/core/detailHealth';
import type { DetailPort } from '@features/detail/ports/detailPort';
import { useDetailPort } from '@features/detail/view/DetailPortProvider';
import { trackExtras } from '@features/detail/core/trackExtras';

type EnrichmentParams = {
  kind: DiscoveryKind;
  title: string;
  subtitle?: string | null | undefined;
  mbid?: string | undefined;
  enabled: boolean;
};

type EnrichmentReturn<T> = {
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

type MbidAwareness = { mbidAware: boolean };

type EnrichmentHookConfig<T> = MbidAwareness & {
  provider: EnrichmentProvider;
  fetch: (port: DetailPort, params: EnrichmentFetchParams) => Promise<T>;
};

type EnrichmentRequest<T> = {
  config: EnrichmentHookConfig<T>;
  port: DetailPort;
  params: EnrichmentParams;
};

type EnrichmentReporter = { port: DetailPort; ctx: EnrichmentFetchContext };

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function reportEnrichmentFailure({ port, ctx }: EnrichmentReporter, error: unknown): void {
  if (isAbort(error)) return;
  recordEnrichmentOutcome(ctx.provider, false);
  port.warn('[detail] enrichment fetch failed', { ...ctx, error: errorMessage(error) });
}

async function fetchReportingOutcome<T>(reporter: EnrichmentReporter, load: () => Promise<T>) {
  try {
    const enrichment = await load();
    recordEnrichmentOutcome(reporter.ctx.provider, true);
    return enrichment;
  } catch (error) {
    reportEnrichmentFailure(reporter, error);
    throw error;
  }
}

function enrichmentMbid(config: MbidAwareness, params: EnrichmentParams) {
  return config.mbidAware && params.mbid ? params.mbid : null;
}

function enrichmentCacheKey(config: MbidAwareness, params: EnrichmentParams) {
  return enrichmentMbid(config, params) ?? `${params.title}|${params.subtitle ?? ''}`;
}

function hasLookupKey(config: MbidAwareness, params: EnrichmentParams) {
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
    fetchReportingOutcome({ port, ctx: fetchContext(config.provider, params) }, () =>
      config.fetch(port, { ...params, signal }),
    );
}

function enrichmentQueryKey<T>(config: EnrichmentHookConfig<T>, params: EnrichmentParams) {
  return detailKeys.enrichment(config.provider, params.kind, enrichmentCacheKey(config, params));
}

function enrichmentQueryOptions<T>(request: EnrichmentRequest<T>) {
  const { config, params } = request;
  return {
    queryKey: enrichmentQueryKey(config, params),
    queryFn: enrichmentQueryFn(request),
    enabled: params.enabled && hasLookupKey(config, params),
    staleTime: STALE.day,
  };
}

function useProviderEnrichment<T extends HasContent>(
  config: EnrichmentHookConfig<T>,
  params: EnrichmentParams,
): EnrichmentReturn<T> {
  const port = useDetailPort();
  const options = enrichmentQueryOptions({ config, port, params });
  const { data: fetched, isError } = useQuery<T>(options);
  return { enrichment: visibleEnrichment(fetched), isError };
}

type MusicBrainzEnrichment = Awaited<ReturnType<DetailPort['getEnrichment']>>;

const MUSICBRAINZ_CONFIG: EnrichmentHookConfig<MusicBrainzEnrichment> = {
  provider: 'musicbrainz',
  fetch: (port, { kind, title, subtitle, mbid, signal }) =>
    port.getEnrichment({ kind, title, subtitle, mbid, signal }),
  mbidAware: true,
};

function useEnrichment(params: EnrichmentParams) {
  return useProviderEnrichment(MUSICBRAINZ_CONFIG, params);
}

type DeezerEnrichment = Awaited<ReturnType<DetailPort['getDeezerEnrichment']>>;

const DEEZER_CONFIG: EnrichmentHookConfig<DeezerEnrichment> = {
  provider: 'deezer',
  fetch: (port, { kind, title, subtitle, signal }) =>
    port.getDeezerEnrichment({ kind, title, subtitle, signal }),
  mbidAware: false,
};

function useDeezerEnrichment(params: EnrichmentParams) {
  return useProviderEnrichment(DEEZER_CONFIG, params);
}

export type EnrichmentErrors = Record<EnrichmentProvider, boolean>;

export type DetailEnrichments = {
  musicbrainz: EnrichmentResponse | null;
  deezer: DeezerEnrichmentResponse | null;
  errors: EnrichmentErrors;
};

export function useDetailEnrichments(result: DiscoveryResult): DetailEnrichments {
  const { kind, title, subtitle } = result;
  const isTrack = kind === 'track';
  const isAlbum = kind === 'album';
  const mbid = trackExtras(result.extras).mbid ?? undefined;

  const mb = useEnrichment({ kind, title, subtitle, mbid, enabled: isTrack || isAlbum });
  const dz = useDeezerEnrichment({ kind, title, subtitle, enabled: isTrack || isAlbum });

  return {
    musicbrainz: mb.enrichment,
    deezer: dz.enrichment,
    errors: { musicbrainz: mb.isError, deezer: dz.isError },
  };
}
