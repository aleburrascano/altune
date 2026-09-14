import { useQuery } from '@tanstack/react-query';

import { getAlbumTracks } from '@shared/api-client/enrichment';
import type { DiscoveryResult } from '@shared/api-client/discovery';

import { resolveEntityQuery } from '../resolve-entity-query';

// Bound oversized tracklists so a discovery-driven album fetch stays capped,
// consistent with useAlbumTracks and the artist albums limit.
const ALBUM_TRACKS_LIMIT = 100;

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

  const {
    data,
    isLoading: isSearching,
    isError: isSearchError,
  } = useQuery({
    ...resolveEntityQuery('album', searchQuery, 1),
    enabled,
  });
  const searchResult = data?.[0] ?? null;

  const source = searchResult?.sources[0];

  const {
    data: tracksData,
    isLoading: isLoadingTracks,
    isError: isTracksError,
    refetch,
  } = useQuery({
    queryKey: ['album-discovery-tracks', source?.provider, source?.external_id],
    queryFn: ({ signal }) =>
      getAlbumTracks(
        source!.provider,
        source!.external_id,
        ALBUM_TRACKS_LIMIT,
        searchResult?.title,
        searchResult?.subtitle ?? undefined,
        undefined,
        signal,
      ),
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
