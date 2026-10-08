import { useQuery } from '@tanstack/react-query';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import {
  contentFailure,
  DETAIL_CONTENT_STALE_MS,
  DETAIL_LIST_CAP,
} from '@/features/detail/domain/content-status';
import { fetchTallyingOutcome } from '@/features/detail/domain/detailHealth';
import { resolveEntityQuery } from '@/features/detail/domain/resolveEntityQuery';
import { useDetailPort } from '@/features/detail/ports/DetailPortProvider';
import { useDetailFetchEnabled, useGatedRefetch } from '@/features/detail/hooks/detailFetchGate';
import { useContentFetchRetry } from '@/features/detail/hooks/useContentFetchRetry';
import { useLoggedSearchFailure } from '@/features/detail/hooks/useLoggedSearchFailure';

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
  const isFetchEnabled = useDetailFetchEnabled();
  const port = useDetailPort();
  const canFetch = enabled && isFetchEnabled;

  const {
    data,
    isLoading: isSearching,
    isError: isSearchError,
    error: searchError,
    refetch: refetchSearch,
  } = useQuery({
    ...resolveEntityQuery('album', searchQuery, 1, port),
    enabled: canFetch,
  });
  const searchResult = data?.[0] ?? null;

  useLoggedSearchFailure(searchError, { kind: 'album', title: albumTitle, artist });

  const searchFailure = contentFailure(isSearchError, searchError, null);

  const source = searchResult?.sources[0];
  const retry = useContentFetchRetry();

  const {
    data: tracksData,
    isLoading: isLoadingTracks,
    isError: isTracksQueryError,
    error: tracksError,
    refetch: refetchTracks,
  } = useQuery({
    queryKey: ['album-discovery-tracks', source?.provider, source?.external_id],
    queryFn: ({ signal }) =>
      fetchTallyingOutcome('album_tracks', () =>
        port.getAlbumTracks({
          provider: source!.provider,
          externalId: source!.external_id,
          limit: DETAIL_LIST_CAP,
          albumTitle: searchResult?.title,
          albumArtist: searchResult?.subtitle ?? undefined,
          signal,
        }),
      ),
    enabled: canFetch && source != null,
    staleTime: DETAIL_CONTENT_STALE_MS,
    retry,
  });

  const tracks: DiscoveryResult[] = tracksData?.items ?? [];
  const tracksFailure = contentFailure(isTracksQueryError, tracksError, tracksData);

  const failure = searchFailure ?? tracksFailure;

  const refetch = useGatedRefetch(() => {
    void refetchSearch();
    void refetchTracks();
  });

  return {
    tracks,
    isLoading: isSearching || isLoadingTracks,
    failure,
    isError: failure !== null,
    refetch,
  };
}
