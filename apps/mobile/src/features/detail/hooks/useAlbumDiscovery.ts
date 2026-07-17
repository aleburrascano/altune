import { useQuery } from '@tanstack/react-query';

import { getAlbumTracks } from '@shared/api-client/enrichment';
import type { DiscoveryResult } from '@shared/api-client/discovery';

import { resolveEntityQuery } from '../resolve-entity-query';

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

  const { data, isLoading: isSearching, isError: isSearchError } = useQuery({
    ...resolveEntityQuery('album', searchQuery, 1),
    enabled,
  });
  const searchResult = data?.[0] ?? null;

  const source = searchResult?.sources[0];

  const { data: tracksData, isLoading: isLoadingTracks, isError: isTracksError, refetch } = useQuery({
    queryKey: ['album-discovery-tracks', source?.provider, source?.external_id],
    queryFn: () => getAlbumTracks(source!.provider, source!.external_id, undefined, searchResult?.title, searchResult?.subtitle ?? undefined),
    enabled: enabled && source != null,
    staleTime: 30 * 60 * 1000,
  });

  const tracks: DiscoveryResult[] = tracksData?.items ?? [];

  return {
    albumResult: searchResult,
    tracks,
    isLoading: isSearching || isLoadingTracks,
    isError: isSearchError || isTracksError,
    refetch,
  };
}
