/**
 * useAlbumTracks — fetch tracks from an album by provider + external ID.
 *
 * AC#14: Fetches from the first source in the album's sources array.
 * Cached per (provider, external_id) for the session.
 */

import { useQuery } from '@tanstack/react-query';

import { getAlbumTracks, type DiscoveryResult } from '@shared/api-client/discovery';

type UseAlbumTracksParams = {
  provider: string;
  externalId: string;
  enabled?: boolean;
};

type UseAlbumTracksReturn = {
  tracks: DiscoveryResult[];
  isLoading: boolean;
  isError: boolean;
  refetch: () => void;
};

export function useAlbumTracks({
  provider,
  externalId,
  enabled = true,
}: UseAlbumTracksParams): UseAlbumTracksReturn {
  const query = useQuery({
    queryKey: ['album-tracks', provider, externalId],
    queryFn: () => getAlbumTracks(provider, externalId),
    enabled,
    staleTime: 1000 * 60 * 30, // 30 minutes
  });

  return {
    tracks: query.data?.items ?? [],
    isLoading: query.isLoading,
    isError: query.isError || query.data?.status === 'error',
    refetch: query.refetch,
  };
}
