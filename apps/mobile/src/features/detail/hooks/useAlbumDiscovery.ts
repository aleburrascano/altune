import { useQuery } from '@tanstack/react-query';

import { searchDiscovery, getAlbumTracks } from '@shared/api-client/discovery';
import type { DiscoveryResult } from '@shared/api-client/discovery';

export function useAlbumDiscovery({
  albumTitle,
  artist,
  enabled,
}: {
  albumTitle: string;
  artist: string | null;
  enabled: boolean;
}) {
  const searchQuery = `${albumTitle} ${artist ?? ''}`.trim();

  const { data: searchResult, isLoading: isSearching, isError: isSearchError } = useQuery({
    queryKey: ['album-discovery-search', searchQuery],
    queryFn: async () => {
      const res = await searchDiscovery({
        q: searchQuery,
        kinds: ['album'],
        limit: 1,
        saveHistory: false,
      });
      return res.results[0] ?? null;
    },
    enabled,
    staleTime: 30 * 60 * 1000,
  });

  const source = searchResult?.sources[0];

  const { data: tracksData, isLoading: isLoadingTracks, isError: isTracksError, refetch } = useQuery({
    queryKey: ['album-discovery-tracks', source?.provider, source?.external_id],
    queryFn: () => getAlbumTracks(source!.provider, source!.external_id, undefined, searchResult?.title, searchResult?.subtitle),
    enabled: enabled && source != null,
    staleTime: 30 * 60 * 1000,
  });

  const tracks: DiscoveryResult[] = tracksData?.items ?? [];

  return {
    albumResult: searchResult ?? null,
    tracks,
    isLoading: isSearching || isLoadingTracks,
    isError: isSearchError || isTracksError,
    refetch,
  };
}
