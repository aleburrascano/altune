import { useQuery } from '@tanstack/react-query';

import { getAlbumTracks } from '@shared/api-client/enrichment';
import type { DiscoveryResult, DiscoverySource } from '@shared/api-client/discovery';
import { detailKeys } from '@shared/lib/query-keys';

import { contentFailure, DETAIL_LIST_CAP, type ContentFailure } from '../content-status';
import { fetchTallyingOutcome } from '../detailHealth';
import { useDetailFetchEnabled, useGatedRefetch } from './detailFetchGate';
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
  const isFetchEnabled = useDetailFetchEnabled();

  const { data, isLoading, isError, error, refetch } = useQuery({
    queryKey: detailKeys.albumTracks(provider, externalId, mbExternalId),
    queryFn: ({ signal }) =>
      fetchTallyingOutcome('album_tracks', () =>
        getAlbumTracks({
          provider,
          externalId,
          limit: DETAIL_LIST_CAP,
          albumTitle,
          albumArtist,
          mbExternalId,
          signal,
        }),
      ),
    enabled: enabled && isFetchEnabled,
    staleTime: 1000 * 60 * 30,
    retry,
  });

  const failure = contentFailure(isError, error, data);
  const retryFetch = useGatedRefetch(refetch);

  return {
    tracks: data?.items ?? [],
    isLoading,
    isError: failure !== null,
    failure,
    refetch: retryFetch,
  };
}
