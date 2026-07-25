import { useQuery } from '@tanstack/react-query';

import { getArtistContent } from '@shared/api-client/enrichment';
import type { DiscoveryResult, DiscoverySource } from '@shared/api-client/discovery';

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
    queryFn: () =>
      getArtistContent(source!.provider, source!.external_id, {
        ...(artistName ? { artistName } : {}),
        tracksLimit: TOP_TRACKS_LIMIT,
        albumsLimit: ALBUMS_LIMIT,
      }),
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
