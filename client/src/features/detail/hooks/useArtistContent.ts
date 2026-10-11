import { useQuery } from '@tanstack/react-query';

import { isAbort } from '@shared/failure/errors';
import type { ArtistContentOptions, ArtistContentResponse } from '@shared/apiClient/enrichment';
import { sourceIdList } from '@shared/apiClient/ids';
import type { DiscoveryResult, DiscoverySource } from '@shared/apiClient/discovery';

import {
  contentFailure,
  DETAIL_CONTENT_STALE_MS,
  DETAIL_LIST_CAP,
  hasDegradedStatus,
  type ContentFailure,
} from '@/features/detail/domain/contentStatus';
import { recordContentFetchOutcome } from '@/features/detail/domain/detailHealth';
import type { DetailPort } from '@/features/detail/ports/detailPort';
import { useDetailPort } from '@/features/detail/ports/DetailPortProvider';
import {
  useDetailFetchEnabled,
  useGatedRefetch,
  useContentFetchRetry,
} from '@/features/detail/hooks/useDetailFetchGate';

type ContentFetchContext = {
  provider: string;
  externalId: string;
  artistName: string | null;
  knownIds: readonly DiscoverySource[];
};

function logContentStatuses(content: ArtistContentResponse, ctx: ContentFetchContext): void {
  const where = logContext(ctx);
  if (content.top_tracks.status !== 'ok') {
    console.warn('[detail] artist top_tracks fetch degraded', {
      ...where,
      status: content.top_tracks.status,
    });
  }
  if (content.albums.status !== 'ok') {
    console.warn('[detail] artist albums fetch degraded', {
      ...where,
      status: content.albums.status,
    });
  }
}

function logContext({ provider, externalId, artistName }: ContentFetchContext) {
  return { provider, externalId, artistName };
}

function isFullyServed(content: ArtistContentResponse): boolean {
  return !hasDegradedStatus(content.top_tracks) && !hasDegradedStatus(content.albums);
}

type UseArtistContentParams = {
  sources: DiscoverySource[];
  artistName?: string;
  enabled?: boolean;
};

type UseArtistContentReturn = {
  topTracks: DiscoveryResult[];
  albums: DiscoveryResult[];
  isLoading: boolean;
  isErrorAlbums: boolean;
  tracksFailure: ContentFailure | null;
  albumsFailure: ContentFailure | null;
  refetch: () => void;
};

const TOP_TRACKS_LIMIT = 5;

function reportContentFailure(error: unknown, ctx: ContentFetchContext): void {
  if (isAbort(error)) return;
  recordContentFetchOutcome('artist_content', false);
  console.warn('[detail] artist content fetch failed', {
    ...logContext(ctx),
    error: error instanceof Error ? error.message : String(error),
  });
}

function contentParams(ctx: ContentFetchContext): ArtistContentOptions {
  const params: ArtistContentOptions = {
    tracksLimit: TOP_TRACKS_LIMIT,
    albumsLimit: DETAIL_LIST_CAP,
    knownIds: ctx.knownIds,
  };
  if (ctx.artistName) params.artistName = ctx.artistName;
  return params;
}

function recordContent(content: ArtistContentResponse, ctx: ContentFetchContext): void {
  logContentStatuses(content, ctx);
  recordContentFetchOutcome('artist_content', isFullyServed(content));
}

async function fetchContent(port: DetailPort, ctx: ContentFetchContext, signal: AbortSignal) {
  const params = contentParams(ctx);
  const content = await port.getArtistContent(ctx.provider, ctx.externalId, params, signal);
  recordContent(content, ctx);
  return content;
}

function contentContext(
  seed: DiscoverySource,
  sources: readonly DiscoverySource[],
  artistName: string | undefined,
): ContentFetchContext {
  return {
    provider: seed.provider,
    externalId: seed.external_id,
    artistName: artistName ?? null,
    knownIds: sources,
  };
}

async function fetchReporting(port: DetailPort, ctx: ContentFetchContext, signal: AbortSignal) {
  try {
    return await fetchContent(port, ctx, signal);
  } catch (error) {
    reportContentFailure(error, ctx);
    throw error;
  }
}

function loadArtistContent(
  port: DetailPort,
  sources: readonly [DiscoverySource, ...DiscoverySource[]],
  artistName: string | undefined,
  signal: AbortSignal,
) {
  return fetchReporting(port, contentContext(sources[0], sources, artistName), signal);
}

export function useArtistContent({
  sources,
  artistName,
  enabled = true,
}: UseArtistContentParams): UseArtistContentReturn {
  const source = sources[0] ?? null;
  const port = useDetailPort();
  const retry = useContentFetchRetry();
  const isFetchEnabled = useDetailFetchEnabled();

  const { data, isLoading, isError, error, refetch } = useQuery({
    queryKey: [
      'artist-content',
      source?.provider ?? '',
      source?.external_id ?? '',
      artistName ?? '',
      sourceIdList(sources),
    ],
    queryFn: ({ signal }) =>
      loadArtistContent(port, [source!, ...sources.slice(1)], artistName, signal),
    enabled: enabled && isFetchEnabled && source !== null,
    staleTime: DETAIL_CONTENT_STALE_MS,
    retry,
  });

  const refetchBoth = useGatedRefetch(refetch);

  const tracksFailure = contentFailure(isError, error, data?.top_tracks);
  const albumsFailure = contentFailure(isError, error, data?.albums);

  return {
    topTracks: data?.top_tracks.status === 'ok' ? data.top_tracks.items : [],
    albums: data?.albums.status === 'ok' ? data.albums.items : [],
    isLoading,
    isErrorAlbums: albumsFailure !== null,
    tracksFailure,
    albumsFailure,
    refetch: refetchBoth,
  };
}
