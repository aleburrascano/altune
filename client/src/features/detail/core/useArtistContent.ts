import { useQuery } from '@tanstack/react-query';

import { isAbort } from '@shared/failure';
import type { ArtistContentOptions, ArtistContentResponse } from '@shared/apiClient';
import type { DiscoveryResult, DiscoverySource } from '@shared/catalog';
import { sourceIdList } from '@shared/catalog';
import { detailKeys } from '@shared/query';

import {
  contentFailure,
  DETAIL_CONTENT_STALE_MS,
  DETAIL_LIST_CAP,
  hasDegradedStatus,
  type ContentFailure,
} from '@features/detail/core/contentStatus';
import { recordContentFetchOutcome } from '@features/detail/core/detailHealth';
import type { DetailPort } from '@features/detail/ports/detailPort';
import { useDetailPort } from '@features/detail/view/DetailPortProvider';
import {
  useDetailFetchEnabled,
  useGatedRefetch,
  useContentFetchRetry,
} from '@features/detail/core/useDetailFetchGate';

type ContentFetchContext = {
  provider: string;
  externalId: string;
  knownIds: readonly DiscoverySource[];
};

type ContentReport = {
  port: DetailPort;
  content: ArtistContentResponse;
  ctx: ContentFetchContext;
};

type ContentSide = 'top_tracks' | 'albums';

function warnIfDegraded({ port, content, ctx }: ContentReport, side: ContentSide): void {
  const { status } = content[side];
  if (status === 'ok') return;
  port.warn(`[detail] artist ${side} fetch degraded`, { ...logContext(ctx), status });
}

function logContentStatuses(report: ContentReport): void {
  warnIfDegraded(report, 'top_tracks');
  warnIfDegraded(report, 'albums');
}

function logContext({ provider, externalId }: ContentFetchContext) {
  return { provider, externalId };
}

function isFullyServed(content: ArtistContentResponse): boolean {
  return !hasDegradedStatus(content.top_tracks) && !hasDegradedStatus(content.albums);
}

type UseArtistContentParams = {
  sources: DiscoverySource[];
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

function reportContentFailure(port: DetailPort, error: unknown, ctx: ContentFetchContext): void {
  if (isAbort(error)) return;
  recordContentFetchOutcome('artist_content', false);
  port.warn('[detail] artist content fetch failed', {
    ...logContext(ctx),
    error: error instanceof Error ? error.message : String(error),
  });
}

function contentParams(ctx: ContentFetchContext): ArtistContentOptions {
  return {
    tracksLimit: TOP_TRACKS_LIMIT,
    albumsLimit: DETAIL_LIST_CAP,
    knownIds: ctx.knownIds,
  };
}

function recordContent(report: ContentReport): void {
  logContentStatuses(report);
  recordContentFetchOutcome('artist_content', isFullyServed(report.content));
}

async function fetchContent(port: DetailPort, ctx: ContentFetchContext, signal: AbortSignal) {
  const params = contentParams(ctx);
  const content = await port.getArtistContent(ctx.provider, ctx.externalId, params, signal);
  recordContent({ port, content, ctx });
  return content;
}

function contentContext(
  seed: DiscoverySource,
  sources: readonly DiscoverySource[],
): ContentFetchContext {
  return {
    provider: seed.provider,
    externalId: seed.external_id,
    knownIds: sources,
  };
}

async function fetchReporting(port: DetailPort, ctx: ContentFetchContext, signal: AbortSignal) {
  try {
    return await fetchContent(port, ctx, signal);
  } catch (error) {
    reportContentFailure(port, error, ctx);
    throw error;
  }
}

function loadArtistContent(
  port: DetailPort,
  sources: readonly [DiscoverySource, ...DiscoverySource[]],
  signal: AbortSignal,
) {
  return fetchReporting(port, contentContext(sources[0], sources), signal);
}

export function useArtistContent({
  sources,
  enabled = true,
}: UseArtistContentParams): UseArtistContentReturn {
  const source = sources[0] ?? null;
  const port = useDetailPort();
  const retry = useContentFetchRetry();
  const isFetchEnabled = useDetailFetchEnabled();

  const { data, isLoading, isError, error, refetch } = useQuery({
    queryKey: detailKeys.artistContent(
      source?.provider ?? '',
      source?.external_id ?? '',
      sourceIdList(sources),
    ),
    queryFn: ({ signal }) => loadArtistContent(port, [source!, ...sources.slice(1)], signal),
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
