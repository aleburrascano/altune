import { useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';

import type { DiscoveryKind, DiscoveryResult } from '@shared/api-client/discovery';

import {
  contentFailure,
  DETAIL_CONTENT_STALE_MS,
  DETAIL_LIST_CAP,
} from '@/features/detail/domain/contentStatus';
import { fetchTallyingOutcome } from '@/features/detail/domain/detailHealth';
import { resolveEntityQuery } from '@/features/detail/domain/resolveEntityQuery';
import { useDetailPort } from '@/features/detail/ports/DetailPortProvider';
import {
  useDetailFetchEnabled,
  useGatedRefetch,
  useContentFetchRetry,
} from '@/features/detail/hooks/useDetailFetchGate';

type SearchedEntity = {
  kind: DiscoveryKind;
  title: string;
  artist: string | null;
};

function warnSearchFailure(error: Error, searched: SearchedEntity): void {
  console.warn('[detail] discovery search failed', { ...searched, error: error.message });
}

function useLoggedSearchFailure(error: Error | null, searched: SearchedEntity): void {
  const { kind, title, artist } = searched;
  useEffect(() => {
    if (error === null) {
      return;
    }
    warnSearchFailure(error, { kind, title, artist });
  }, [error, kind, title, artist]);
}

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

const LASTFM_PLACEHOLDER_HASH = '2a96cbd8b46e442fc41c2b86b821562f';

export function useArtistDiscovery({
  artistName,
  enabled,
}: {
  artistName: string;
  enabled: boolean;
}) {
  const isFetchEnabled = useDetailFetchEnabled();
  const port = useDetailPort();

  const { data, isLoading, isError, error, refetch } = useQuery({
    ...resolveEntityQuery('artist', artistName, 1, port),
    enabled: enabled && isFetchEnabled,
  });
  const searchResult = data?.[0] ?? null;
  const retrySearch = useGatedRefetch(refetch);

  useLoggedSearchFailure(error, { kind: 'artist', title: artistName, artist: null });

  const rawImageUrl = searchResult?.image_url ?? null;
  const isPlaceholder = rawImageUrl != null && rawImageUrl.includes(LASTFM_PLACEHOLDER_HASH);
  const imageUrl = isPlaceholder ? null : rawImageUrl;

  return {
    imageUrl,
    sources: searchResult?.sources ?? [],
    isLoading,
    isError,
    refetch: retrySearch,
  };
}
