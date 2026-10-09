import { useQuery } from '@tanstack/react-query';

import type { DiscoveryKind, DiscoveryResult } from '@shared/api-client/discovery';
import { isAbort } from '@shared/failure/errors';
import type {
  DeezerEnrichmentResponse,
  EnrichmentResponse,
  LastFmEnrichmentResponse,
} from '@shared/api-client/enrichment';

import {
  recordEnrichmentOutcome,
  type EnrichmentProvider,
} from '@/features/detail/domain/detailHealth';
import type { DetailPort } from '@/features/detail/ports/detailPort';
import { useDetailPort } from '@/features/detail/ports/DetailPortProvider';
import { useDetailFetchEnabled } from '@/features/detail/hooks/useDetailFetchGate';
import { trackExtras } from '@/features/detail/domain/trackExtras';

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

type MusicBrainzEnrichment = Awaited<ReturnType<DetailPort['getEnrichment']>>;

const MUSICBRAINZ_CONFIG: EnrichmentHookConfig<MusicBrainzEnrichment> = {
  keyPrefix: 'enrichment',
  provider: 'musicbrainz',
  fetch: (port, { kind, title, subtitle, mbid, signal }) =>
    port.getEnrichment({ kind, title, subtitle, mbid, signal }),
  mbidAware: true,
};

export function useEnrichment(params: EnrichmentParams) {
  return useProviderEnrichment(MUSICBRAINZ_CONFIG, params);
}

type DeezerEnrichment = Awaited<ReturnType<DetailPort['getDeezerEnrichment']>>;

const DEEZER_CONFIG: EnrichmentHookConfig<DeezerEnrichment> = {
  keyPrefix: 'deezer-enrichment',
  provider: 'deezer',
  fetch: (port, { kind, title, subtitle, signal }) =>
    port.getDeezerEnrichment({ kind, title, subtitle, signal }),
  mbidAware: false,
};

export function useDeezerEnrichment(params: EnrichmentParams) {
  return useProviderEnrichment(DEEZER_CONFIG, params);
}

type LastFmEnrichment = Awaited<ReturnType<DetailPort['getLastFmEnrichment']>>;

const LASTFM_CONFIG: EnrichmentHookConfig<LastFmEnrichment> = {
  keyPrefix: 'lastfm-enrichment',
  provider: 'lastfm',
  fetch: (port, { kind, title, subtitle, signal }) =>
    port.getLastFmEnrichment({ kind, title, subtitle, signal }),
  mbidAware: false,
};

export function useLastFmEnrichment(params: EnrichmentParams) {
  return useProviderEnrichment(LASTFM_CONFIG, params);
}

export type EnrichmentErrors = Record<EnrichmentProvider, boolean>;

export type DetailEnrichments = {
  musicbrainz: EnrichmentResponse | null;
  deezer: DeezerEnrichmentResponse | null;
  lastfm: LastFmEnrichmentResponse | null;
  errors: EnrichmentErrors;
};

export function useDetailEnrichments(result: DiscoveryResult): DetailEnrichments {
  const { kind, title, subtitle } = result;
  const isTrack = kind === 'track';
  const isAlbum = kind === 'album';
  const isArtist = kind === 'artist';
  const mbid = trackExtras(result.extras).mbid ?? undefined;

  const mb = useEnrichment({ kind, title, subtitle, mbid });
  const dz = useDeezerEnrichment({ kind, title, subtitle, enabled: isTrack || isAlbum });
  const lf = useLastFmEnrichment({ kind, title, subtitle, enabled: isArtist });

  return {
    musicbrainz: mb.enrichment,
    deezer: dz.enrichment,
    lastfm: lf.enrichment,
    errors: { musicbrainz: mb.isError, deezer: dz.isError, lastfm: lf.isError },
  };
}
