import { useQuery } from '@tanstack/react-query';

import { getArtistContent } from '@shared/api-client/enrichment';
import type { ArtistContentResponse } from '@shared/api-client/enrichment';
import type { DiscoveryResult, DiscoverySource } from '@shared/api-client/discovery';

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
  refetchTracks: () => void;
  refetchAlbums: () => void;
};

const CONTENT_STALE_MS = 30 * 60 * 1000;
const ALBUMS_LIMIT = 100;
const TOP_TRACKS_LIMIT = 5;

export function useArtistContent({
  sources,
  artistName,
  enabled = true,
}: UseArtistContentParams): UseArtistContentReturn {
  const source = sources[0] ?? null;

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: [
      'artist-content',
      source?.provider ?? '',
      source?.external_id ?? '',
      artistName ?? '',
    ],
    queryFn: async () => {
      const ctx: ContentFetchContext = {
        provider: source!.provider,
        externalId: source!.external_id,
        artistName: artistName ?? null,
      };
      try {
        const content = await getArtistContent(source!.provider, source!.external_id, {
          ...(artistName ? { artistName } : {}),
          tracksLimit: TOP_TRACKS_LIMIT,
          albumsLimit: ALBUMS_LIMIT,
        });
        logContentStatuses(content, ctx);
        return content;
      } catch (error) {
        console.warn('[detail] artist content fetch failed', {
          ...ctx,
          error: error instanceof Error ? error.message : String(error),
        });
        throw error;
      }
    },
    enabled: enabled && source !== null,
    staleTime: CONTENT_STALE_MS,
  });

  const refetchBoth = (): void => {
    void refetch();
  };

  return {
    topTracks: data?.top_tracks.status === 'ok' ? data.top_tracks.items : [],
    albums: data?.albums.status === 'ok' ? data.albums.items : [],
    isLoadingTracks: isLoading,
    isLoadingAlbums: isLoading,
    isErrorTracks: isError || (data !== undefined && data.top_tracks.status !== 'ok'),
    isErrorAlbums: isError || (data !== undefined && data.albums.status !== 'ok'),
    refetchTracks: refetchBoth,
    refetchAlbums: refetchBoth,
  };
}
