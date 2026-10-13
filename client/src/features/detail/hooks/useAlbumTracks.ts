import { useQuery } from '@tanstack/react-query';

import type { DiscoveryResult, DiscoverySource } from '@shared/apiClient/discovery';
import { sourceIdList } from '@shared/apiClient/ids';
import { detailKeys } from '@shared/query/queryKeys';

import {
  contentFailure,
  DETAIL_CONTENT_STALE_MS,
  DETAIL_LIST_CAP,
  type ContentFailure,
} from '@features/detail/domain/contentStatus';
import { fetchTallyingOutcome } from '@features/detail/domain/detailHealth';
import { useDetailPort } from '@features/detail/ports/DetailPortProvider';
import {
  useDetailFetchEnabled,
  useGatedRefetch,
  useContentFetchRetry,
} from '@features/detail/hooks/useDetailFetchGate';

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
  const port = useDetailPort();
  const retry = useContentFetchRetry();
  const isFetchEnabled = useDetailFetchEnabled();

  const { data, isLoading, isError, error, refetch } = useQuery({
    queryKey: detailKeys.albumTracks(
      provider,
      externalId,
      mbExternalId,
      allSources ? sourceIdList(allSources) : undefined,
    ),
    queryFn: ({ signal }) =>
      fetchTallyingOutcome('album_tracks', () =>
        port.getAlbumTracks({
          provider,
          externalId,
          limit: DETAIL_LIST_CAP,
          albumTitle,
          albumArtist,
          mbExternalId,
          knownIds: allSources,
          signal,
        }),
      ),
    enabled: enabled && isFetchEnabled,
    staleTime: DETAIL_CONTENT_STALE_MS,
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
