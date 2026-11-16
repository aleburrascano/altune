import { useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { DiscoveryKind, DiscoveryResult } from '@shared/catalog';
import { detailKeys } from '@shared/query';

import {
  contentFailure,
  DETAIL_CONTENT_STALE_MS,
  DETAIL_LIST_CAP,
} from '@features/detail/domain/contentStatus';
import { fetchTallyingOutcome } from '@features/detail/domain/detailHealth';
import { resolveEntityQuery } from '@features/detail/domain/resolveEntityQuery';
import { realPhotoUrl } from '@features/detail/domain/artistPhoto';
import type { DetailPort } from '@features/detail/ports/detailPort';
import { useDetailPort } from '@features/detail/view/DetailPortProvider';
import {
  useDetailFetchEnabled,
  useGatedRefetch,
  useContentFetchRetry,
} from '@features/detail/hooks/useDetailFetchGate';

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

function useAlbumSearch(
  albumTitle: string,
  artist: string | null,
  canFetch: boolean,
  port: DetailPort,
) {
  const searchQuery = `${albumTitle} ${artist ?? ''}`.trim();
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
  return { searchResult, isSearching, searchFailure, refetchSearch };
}

type AlbumSource = DiscoveryResult['sources'][number];

function albumTracksQuery(
  source: AlbumSource | undefined,
  searchResult: DiscoveryResult | null,
  canFetch: boolean,
  port: DetailPort,
) {
  return {
    queryKey: detailKeys.albumDiscoveryTracks(source?.provider, source?.external_id),
    queryFn: ({ signal }: { signal: AbortSignal }) =>
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
  };
}

function useDiscoveredAlbumTracks(
  searchResult: DiscoveryResult | null,
  canFetch: boolean,
  port: DetailPort,
) {
  const source = searchResult?.sources[0];
  const retry = useContentFetchRetry();

  const {
    data: tracksData,
    isLoading: isLoadingTracks,
    isError: isTracksQueryError,
    error: tracksError,
    refetch: refetchTracks,
  } = useQuery({
    ...albumTracksQuery(source, searchResult, canFetch, port),
    retry,
  });

  const tracks: DiscoveryResult[] = tracksData?.items ?? [];
  const tracksFailure = contentFailure(isTracksQueryError, tracksError, tracksData);
  return { tracks, isLoadingTracks, tracksFailure, refetchTracks };
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
  const isFetchEnabled = useDetailFetchEnabled();
  const port = useDetailPort();
  const canFetch = enabled && isFetchEnabled;

  const { searchResult, isSearching, searchFailure, refetchSearch } = useAlbumSearch(
    albumTitle,
    artist,
    canFetch,
    port,
  );
  const { tracks, isLoadingTracks, tracksFailure, refetchTracks } = useDiscoveredAlbumTracks(
    searchResult,
    canFetch,
    port,
  );

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

function artistSearchFields(searchResult: DiscoveryResult | null) {
  return {
    imageUrl: realPhotoUrl(searchResult?.image_url),
    sources: searchResult?.sources ?? [],
  };
}

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

  return {
    ...artistSearchFields(searchResult),
    isLoading,
    isError,
    refetch: retrySearch,
  };
}
