import { useEffect } from 'react';
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import type { DiscoveryKind, DiscoveryResult } from '@shared/catalog';
import { detailKeys } from '@shared/query';

import {
  contentFailure,
  DETAIL_CONTENT_STALE_MS,
  DETAIL_LIST_CAP,
  type ContentFailure,
} from '@features/detail/core/contentStatus';
import { fetchTallyingOutcome } from '@features/detail/core/detailHealth';
import { resolveEntityQuery } from '@features/detail/core/resolveEntityQuery';
import { realPhotoUrl } from '@features/detail/core/artistPhoto';
import type { DetailPort } from '@features/detail/ports/detailPort';
import { useDetailPort } from '@features/detail/view/DetailPortProvider';
import { useContentFetchRetry } from '@features/detail/core/useContentFetchRetry';

type ReadQueryFields<T> = Pick<
  UseQueryResult<T>,
  'data' | 'isLoading' | 'isError' | 'error' | 'refetch'
>;

type SearchedEntity = {
  kind: DiscoveryKind;
  title: string;
  artist: string | null;
};

function warnSearchFailure(port: DetailPort, error: Error, searched: SearchedEntity): void {
  port.warn('[detail] discovery search failed', { ...searched, error: error.message });
}

function useLoggedSearchFailure(error: Error | null, searched: SearchedEntity): void {
  const port = useDetailPort();
  const { kind, title, artist } = searched;
  useEffect(() => {
    if (error === null) {
      return;
    }
    warnSearchFailure(port, error, { kind, title, artist });
  }, [port, error, kind, title, artist]);
}

type AlbumSearchInput = {
  albumTitle: string;
  artist: string | null;
  canFetch: boolean;
  port: DetailPort;
};

type AlbumSearch = {
  searchResult: DiscoveryResult | null;
  isSearching: boolean;
  searchFailure: ContentFailure | null;
  refetchSearch: () => Promise<unknown>;
};

function albumSearchQuery({ albumTitle, artist, canFetch, port }: AlbumSearchInput) {
  const searchQuery = `${albumTitle} ${artist ?? ''}`.trim();
  return { ...resolveEntityQuery('album', searchQuery, 1, port), enabled: canFetch };
}

function albumSearchOf(search: ReadQueryFields<DiscoveryResult[]>): AlbumSearch {
  return {
    searchResult: search.data?.[0] ?? null,
    isSearching: search.isLoading,
    searchFailure: contentFailure(search.isError, search.error, null),
    refetchSearch: search.refetch,
  };
}

function useAlbumSearch(input: AlbumSearchInput): AlbumSearch {
  const { data: found, isLoading, isError, error, refetch } = useQuery(albumSearchQuery(input));
  const searched = { kind: 'album' as const, title: input.albumTitle, artist: input.artist };
  useLoggedSearchFailure(error, searched);
  return albumSearchOf({ data: found, isLoading, isError, error, refetch });
}

type AlbumSource = DiscoveryResult['sources'][number];

type AlbumTracksPage = Awaited<ReturnType<DetailPort['getAlbumTracks']>>;

type QuerySignal = { signal: AbortSignal };

type AlbumTracksInput = {
  searchResult: DiscoveryResult | null;
  canFetch: boolean;
  port: DetailPort;
};

type AlbumTracksQueryInput = AlbumTracksInput & { source: AlbumSource | undefined };

type AlbumTracksRequestInput = QuerySignal & {
  searchResult: DiscoveryResult | null;
  source: AlbumSource;
};

type DiscoveredTracks = {
  tracks: DiscoveryResult[];
  isLoadingTracks: boolean;
  tracksFailure: ContentFailure | null;
  refetchTracks: () => Promise<unknown>;
};

function albumTracksRequest({ searchResult, source, signal }: AlbumTracksRequestInput) {
  return {
    provider: source.provider,
    externalId: source.external_id,
    limit: DETAIL_LIST_CAP,
    albumTitle: searchResult?.title,
    albumArtist: searchResult?.subtitle ?? undefined,
    signal,
  };
}

function albumTracksQueryFn({ searchResult, source, port }: AlbumTracksQueryInput) {
  return ({ signal }: QuerySignal) =>
    fetchTallyingOutcome('album_tracks', () =>
      port.getAlbumTracks(albumTracksRequest({ searchResult, source: source!, signal })),
    );
}

function albumTracksQuery(input: AlbumTracksQueryInput) {
  const { source } = input;
  return {
    queryKey: detailKeys.albumDiscoveryTracks(source?.provider, source?.external_id),
    queryFn: albumTracksQueryFn(input),
    enabled: input.canFetch && source != null,
    staleTime: DETAIL_CONTENT_STALE_MS,
  };
}

function discoveredTracksOf(query: ReadQueryFields<AlbumTracksPage>): DiscoveredTracks {
  const { data: tracksData, isLoading: isLoadingTracks, refetch: refetchTracks } = query;
  const tracks: DiscoveryResult[] = tracksData?.items ?? [];
  const tracksFailure = contentFailure(query.isError, query.error, tracksData);
  return { tracks, isLoadingTracks, tracksFailure, refetchTracks };
}

function useDiscoveredAlbumTracks(input: AlbumTracksInput): DiscoveredTracks {
  const source = input.searchResult?.sources[0];
  const retry = useContentFetchRetry();
  const options = { ...albumTracksQuery({ ...input, source }), retry };
  const { data: page, isLoading, isError, error, refetch } = useQuery(options);
  return discoveredTracksOf({ data: page, isLoading, isError, error, refetch });
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
  const port = useDetailPort();

  const { searchResult, isSearching, searchFailure, refetchSearch } = useAlbumSearch({
    albumTitle,
    artist,
    canFetch: enabled,
    port,
  });
  const { tracks, isLoadingTracks, tracksFailure, refetchTracks } = useDiscoveredAlbumTracks({
    searchResult,
    canFetch: enabled,
    port,
  });

  const failure = searchFailure ?? tracksFailure;

  function refetch(): void {
    void refetchSearch();
    void refetchTracks();
  }

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
  const port = useDetailPort();

  const { data, isLoading, isError, error, refetch } = useQuery({
    ...resolveEntityQuery('artist', artistName, 1, port),
    enabled,
  });
  const searchResult = data?.[0] ?? null;

  useLoggedSearchFailure(error, { kind: 'artist', title: artistName, artist: null });

  return {
    ...artistSearchFields(searchResult),
    isLoading,
    isError,
    refetch: () => void refetch(),
  };
}
