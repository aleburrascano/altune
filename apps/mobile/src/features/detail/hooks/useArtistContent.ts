import { useQuery } from '@tanstack/react-query';

import { getArtistContent } from '@shared/api-client/enrichment';
import { isAbort } from '@shared/errors';
import type { ArtistContentResponse } from '@shared/api-client/enrichment';
import type { DiscoveryResult, DiscoverySource } from '@shared/api-client/discovery';

import {
  contentFailure,
  DETAIL_LIST_CAP,
  hasDegradedStatus,
  type ContentFailure,
} from '../content-status';
import { recordContentFetchOutcome } from '../detailHealth';
import { useDetailFetchEnabled, useGatedRefetch } from './detailFetchGate';
import { useContentFetchRetry } from './useContentFetchRetry';

// A discovery request that yielded a response but with a degraded per-provider
// status still collapses into `isErrorTracks`/`isErrorAlbums` for the UI. Log
// the status/provider/artist here so an incident can be diagnosed without a
// live repro of which side (top tracks vs albums) or which provider failed.
type ContentFetchContext = {
  provider: string;
  externalId: string;
  artistName: string | null;
};

function logContentStatuses(content: ArtistContentResponse, ctx: ContentFetchContext): void {
  if (content.top_tracks.status !== 'ok') {
    console.warn('[detail] artist top_tracks fetch degraded', {
      ...ctx,
      status: content.top_tracks.status,
    });
  }
  if (content.albums.status !== 'ok') {
    console.warn('[detail] artist albums fetch degraded', {
      ...ctx,
      status: content.albums.status,
    });
  }
}

// One request carries both sides, so either one degraded is a degraded artist-content fetch.
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
  isLoadingTracks: boolean;
  isLoadingAlbums: boolean;
  isErrorTracks: boolean;
  isErrorAlbums: boolean;
  tracksFailure: ContentFailure | null;
  albumsFailure: ContentFailure | null;
  refetchTracks: () => void;
  refetchAlbums: () => void;
};

const CONTENT_STALE_MS = 30 * 60 * 1000;
const TOP_TRACKS_LIMIT = 5;

function reportContentFailure(error: unknown, ctx: ContentFetchContext): void {
  if (isAbort(error)) return;
  recordContentFetchOutcome('artist_content', false);
  console.warn('[detail] artist content fetch failed', {
    ...ctx,
    error: error instanceof Error ? error.message : String(error),
  });
}

async function fetchContent(
  ctx: ContentFetchContext,
  artistName: string | undefined,
  signal: AbortSignal,
) {
  const content = await getArtistContent(
    ctx.provider,
    ctx.externalId,
    {
      ...(artistName ? { artistName } : {}),
      tracksLimit: TOP_TRACKS_LIMIT,
      albumsLimit: DETAIL_LIST_CAP,
    },
    signal,
  );
  logContentStatuses(content, ctx);
  recordContentFetchOutcome('artist_content', isFullyServed(content));
  return content;
}

async function loadArtistContent(
  source: { provider: string; external_id: string },
  artistName: string | undefined,
  signal: AbortSignal,
) {
  const ctx: ContentFetchContext = {
    provider: source.provider,
    externalId: source.external_id,
    artistName: artistName ?? null,
  };
  try {
    return await fetchContent(ctx, artistName, signal);
  } catch (error) {
    reportContentFailure(error, ctx);
    throw error;
  }
}

export function useArtistContent({
  sources,
  artistName,
  enabled = true,
}: UseArtistContentParams): UseArtistContentReturn {
  const source = sources[0] ?? null;
  const retry = useContentFetchRetry();
  const isFetchEnabled = useDetailFetchEnabled();

  const { data, isLoading, isError, error, refetch } = useQuery({
    queryKey: [
      'artist-content',
      source?.provider ?? '',
      source?.external_id ?? '',
      artistName ?? '',
    ],
    queryFn: ({ signal }) => loadArtistContent(source!, artistName, signal),
    enabled: enabled && isFetchEnabled && source !== null,
    staleTime: CONTENT_STALE_MS,
    retry,
  });

  const refetchBoth = useGatedRefetch(refetch);

  const tracksFailure = contentFailure(isError, error, data?.top_tracks);
  const albumsFailure = contentFailure(isError, error, data?.albums);

  return {
    topTracks: data?.top_tracks.status === 'ok' ? data.top_tracks.items : [],
    albums: data?.albums.status === 'ok' ? data.albums.items : [],
    isLoadingTracks: isLoading,
    isLoadingAlbums: isLoading,
    isErrorTracks: tracksFailure !== null,
    isErrorAlbums: albumsFailure !== null,
    tracksFailure,
    albumsFailure,
    refetchTracks: refetchBoth,
    refetchAlbums: refetchBoth,
  };
}
