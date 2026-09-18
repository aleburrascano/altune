import { useQuery } from '@tanstack/react-query';

import { getAlbumTracks } from '@shared/api-client/enrichment';
import type { DiscoveryResult, DiscoverySource } from '@shared/api-client/discovery';

import { contentFailure, DETAIL_LIST_CAP, type ContentFailure } from '../content-status';
import { useContentFetchRetry } from './useContentFetchRetry';

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
  failure: ContentFailure | null;
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
  const retry = useContentFetchRetry();

  const { data, isLoading, isError, error, refetch } = useQuery({
    queryKey: ['album-tracks', provider, externalId, mbExternalId ?? ''],
    queryFn: ({ signal }) =>
      getAlbumTracks(
        provider,
        externalId,
        DETAIL_LIST_CAP,
        albumTitle,
        albumArtist,
        mbExternalId,
        signal,
      ),
    enabled,
    staleTime: 1000 * 60 * 30,
    retry,
  });

  const failure = contentFailure(isError, error, data);

  return {
    tracks: data?.items ?? [],
    isLoading,
    isError: failure !== null,
    failure,
    refetch: () => {
      void refetch();
    },
  };
}
