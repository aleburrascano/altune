import { useQuery } from '@tanstack/react-query';

import { getAlbumTracks } from '@shared/api-client/enrichment';
import type { DiscoveryResult, DiscoverySource } from '@shared/api-client/discovery';

type UseAlbumTracksParams = {
  provider: string;
  externalId: string;
  albumTitle?: string;
  albumArtist?: string | undefined;
  allSources?: DiscoverySource[];
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
  albumTitle,
  albumArtist,
  allSources,
  enabled = true,
}: UseAlbumTracksParams): UseAlbumTracksReturn {
  const mbExternalId = allSources?.find((s) => s.provider === 'musicbrainz')?.external_id;

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ['album-tracks', provider, externalId, mbExternalId ?? ''],
    queryFn: () =>
      getAlbumTracks(provider, externalId, undefined, albumTitle, albumArtist, mbExternalId),
    enabled,
    staleTime: 1000 * 60 * 30,
  });

  return {
    tracks: data?.items ?? [],
    isLoading,
    isError: isError || data?.status === 'error',
    refetch,
  };
}
